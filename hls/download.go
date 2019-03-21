// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package hls

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type output struct {
	client   http.Client
	file     *os.File
	offset   int64
	sequence int
	queue    struct {
		c        chan *segment
		sequence int
	}
}

func (s *stream) processSegment(req *http.Request, seg *segment) (err error) {
	if seg.length == 0 {
		err = fatal(s.processSkippedSegment(seg))
		if err != nil {
			log.Println("Failed to process skipped segment:", err)
		}
		return
	}

	var try uint
	for {
		err = s.downloadSegment(req, seg)
		if err == nil {
			return
		}

		try++
		log.Printf("Failed to download segment %d (try %d): %s\n", seg.sequence, try, err)
		if _, ok := err.(fatalError); ok {
			break
		}

		time.Sleep(time.Duration(64<<min(try, 4)) * time.Millisecond)
		if s.d.stop {
			return
		}
	}

	return
}

func (s *stream) checkMissingSegments(seg *segment) (err error) {
	if s.output.sequence != 0 {
		s.output.sequence++
		if s.output.sequence != seg.sequence {
			log.Printf("Warning: Missing sequence %d-%d\n", s.output.sequence, seg.sequence-1)
			if _, err = fmt.Fprintf(s.playlist.writer, "# WARNING: Missing sequence %d-%d\n",
				s.output.sequence, seg.sequence-1); err != nil {
				return
			}
		}
	}
	s.output.sequence = seg.sequence
	return
}

func (s *stream) processSkippedSegment(seg *segment) (err error) {
	if cl := len(seg.comments); cl > 0 {
		defer s.playlist.flush(&err)

		if err = s.checkMissingSegments(seg); err != nil {
			return
		}

		if _, err = s.playlist.writer.WriteString("# SKIP: "); err != nil {
			return
		}
		if err = writeLine(s.playlist.writer,
			strings.ReplaceAll(seg.comments[:cl-1], "\n", "\n# SKIP: ")); err != nil {
			return
		}
	}
	return
}

func (s *stream) downloadSegment(req *http.Request, seg *segment) (err error) {
	if s.d.Verbose {
		log.Println("Downloading:", seg.uri)
	}

	if req.URL, err = s.playlist.url.Parse(seg.uri); err != nil {
		return
	}
	req.Host = req.URL.Host

	var expectedStatus int
	if seg.length >= 0 && seg.offset >= 0 {
		// Partial request
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.offset, seg.offset+seg.length-1))
		expectedStatus = http.StatusPartialContent
	} else {
		req.Header.Del("Range")
		expectedStatus = http.StatusOK
	}

	s.output.client.Timeout = time.Duration(seg.duration) * time.Duration(s.d.SegmentTimeout) * time.Second
	resp, err := s.output.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		err = httpResponseStatusError(resp)
		return
	}

	outputFile := s.output.file
	if outputFile == nil {
		outputFile, err = createFileWriteOnly(fmt.Sprintf("%s-%d.ts", s.name, seg.sequence))
		if err != nil {
			return
		}
		defer outputFile.Close()
	}

	size, err := io.Copy(outputFile, resp.Body)
	if err != nil {
		if s.d.SingleFile {
			if _, err2 := outputFile.Seek(s.output.offset, io.SeekStart); err2 != nil {
				log.Println("Failed to seek to previous offset:", err2)
				err = fatal(err)
			}
		}

		return
	}

	start := s.output.offset
	s.output.offset += size

	defer s.playlist.flush(&err)

	if err = fatal(s.checkMissingSegments(seg)); err != nil {
		return
	}

	if _, err = s.playlist.writer.WriteString(seg.comments); err != nil {
		err = fatal(err)
		return
	}
	if s.d.SingleFile {
		if _, err = fmt.Fprintf(s.playlist.writer, "#EXT-X-BYTERANGE:%d@%d\n", size, start); err != nil {
			err = fatal(err)
			return
		}
	}
	if err = fatal(writeLine(s.playlist.writer, path.Base(outputFile.Name()))); err != nil {
		return
	}

	return
}

func (s *stream) downloadWorker() (err error) {
	if s.d.SegmentTimeout < 0 {
		s.d.SegmentTimeout = 5
	}

	req, err := s.d.newRequest(s.playlist.url.String())
	if err != nil {
		return
	}

	if s.d.SingleFile {
		if s.output.file, err = createFileWriteOnly(s.name + ".ts"); err != nil {
			log.Println("Failed to create output file", err)
			return
		}
		defer s.output.file.Close()
	}

	if s.d.stop {
		return
	}

	for seg := range s.output.queue.c {
		if s.d.stop {
			return
		}

		if err = s.processSegment(req, seg); err != nil {
			if ferr, ok := err.(fatalError); ok && !ferr.client {
				return
			}
		}
	}
	return
}
