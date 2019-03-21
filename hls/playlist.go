// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package hls

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	tagPrefix    = "#EXT"
	tagSeparator = ':'
)

var segmentTags = map[string]struct{}{
	"EXTINF":                  {},
	"EXT-X-BYTERANGE":         {},
	"EXT-X-DISCONTINUITY":     {},
	"EXT-X-KEY":               {},
	"EXT-X-MAP":               {},
	"EXT-X-PROGRAM-DATE-TIME": {},
	"EXT-X-DATERANGE":         {},
	"EXT-X-GAP":               {},
	"EXT-X-BITRATE":           {},
	// Technically not a segment tag but it appears in the segment section
	"EXT-X-ENDLIST": {},
}

type playlist struct {
	client         http.Client
	url            *url.URL
	file           *os.File
	writer         *bufio.Writer
	version        int
	sequence       int
	targetDuration time.Duration
	lastDuration   time.Duration
	active         bool
	err            error
}

type segment struct {
	sequence int
	duration int
	uri      string
	length   int64
	offset   int64
	comments string
}

var (
	errMissingTargetDuration = fatal(errors.New("playlist is missing EXT-X-TARGETDURATION"))
	errOffsetFirstSegment    = errors.New("offset must be in first segment")
)

func (s *stream) parseHeader(scanner *bufio.Scanner) (err error) {
	if !scanner.Scan() {
		err = eofIfNil(scanner.Err())
		return
	}

	line := scanner.Text()
	if line != "#EXTM3U" {
		err = errors.New("playlist file does not start with #EXTM3U: " + line)
		return
	}

	initial := s.playlist.writer == nil
	if initial {
		s.playlist.writer = bufio.NewWriter(s.playlist.file)
		defer s.playlist.flush(&err)

		if err = fatal(writeLine(s.playlist.writer, line)); err != nil {
			return
		}
	}

	version := 1
	sequence := 0
	var targetDuration time.Duration

loop:
	for scanner.Scan() {
		line = scanner.Text()
		if line == "" {
			continue
		}

		if line[0] != '#' {
			break
		}

		if strings.HasPrefix(line, tagPrefix) {
			k, v := splitPair(line[1:], tagSeparator)
			switch k {
			case "EXT-X-VERSION":
				version, err = strconv.Atoi(v)
				if s.d.SingleFile && version < 4 {
					line = "#EXT-X-VERSION:4" // For byte ranges
				}
			case "EXT-X-TARGETDURATION":
				var duration int
				duration, err = strconv.Atoi(v)
				targetDuration = time.Duration(duration) * time.Second
			case "EXT-X-MEDIA-SEQUENCE":
				sequence, err = strconv.Atoi(v)
			case "EXT-X-PLAYLIST-TYPE":
				if !initial && v == "VOD" {
					s.playlist.active = false
				}
			default:
				if _, ok := segmentTags[k]; ok {
					break loop
				}
			}

			if err != nil {
				err = fatal(fmt.Errorf("invalid %s tag with value '%s': %s", k, v, err))
				return
			}
		}

		if initial {
			if err = fatal(writeLine(s.playlist.writer, line)); err != nil {
				return
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return
	}

	if s.playlist.version != version {
		if s.playlist.version > 0 {
			log.Println("Warning: EXT-X-VERSION changed from", s.playlist.version, "to", version)
		}
		s.playlist.version = version
	}

	if s.playlist.targetDuration == 0 {
		if targetDuration > 0 {
			s.playlist.targetDuration = targetDuration
		} else {
			err = errMissingTargetDuration
			return
		}
	} else if s.playlist.targetDuration != targetDuration {
		log.Println("Warning: EXT-X-TARGETDURATION changed from", s.playlist.targetDuration, "to", targetDuration)
		if targetDuration > 0 {
			s.playlist.targetDuration = targetDuration
		}
	}

	if sequence > s.playlist.sequence {
		s.playlist.sequence = sequence
	} else if sequence != s.playlist.sequence {
		err = fatal(fmt.Errorf("media sequence number decreased from %d to %d", s.playlist.sequence, sequence))
		return
	}
	return
}

func (s *stream) parseSegments(scanner *bufio.Scanner) (err error) {
	sequence := s.playlist.sequence
	newSegments := 0

	var length, offset int64 = -1, -1
	var duration int
	var title string
	var comments strings.Builder

	for ok := true; ok; ok = scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if line == "" || line[0] == '#' {
			if strings.HasPrefix(line, tagPrefix) {
				k, v := splitPair(line[1:], tagSeparator)
				switch k {
				case "EXTINF":
					v, title = splitPair(v, ',')
					v, _ = splitPair(v, '.')
					duration, err = strconv.Atoi(v)
				case "EXT-X-BYTERANGE":
					l, o := splitPair(v, '@')
					if length, err = strconv.ParseInt(l, 10, 64); err != nil {
						break
					}

					if length == 0 {
						log.Println("Warning: Empty segment (length 0)?:", line)
					}

					if o != "" {
						if offset, err = strconv.ParseInt(o, 10, 64); err != nil {
							break
						}
					} else if offset == -1 {
						err = errOffsetFirstSegment
					}

					line = "" // Do not write to output playlist
				case "EXT-X-GAP":
					length = 0
				case "EXT-X-ENDLIST":
					s.playlist.active = false
				}

				if err != nil {
					err = fatal(fmt.Errorf("invalid %s tag with value '%s': %s", k, v, err))
					return
				}
			}

			_ = writeLine(&comments, line)
			continue
		}

		if sequence > s.output.queue.sequence {
			newSegments++

			if len(s.d.Titles) > 0 && !contains(s.d.Titles, title) {
				length = 0 // Skip segment
				log.Println("Skipping segment", sequence, "with title:", title)
			}

			s.output.queue.c <- &segment{
				sequence: sequence,
				duration: duration,
				uri:      line,
				comments: comments.String(),
				length:   length,
				offset:   offset,
			}

			s.output.queue.sequence = sequence
			if duration > 0 {
				s.playlist.lastDuration = time.Duration(duration) * time.Second
			}
		}

		if length > 0 {
			offset += length
		}
		length = -1
		duration = 0
		title = ""
		comments.Reset()
		sequence++
	}

	if err = scanner.Err(); err != nil {
		return
	}

	if s.d.Verbose {
		log.Println("Found", newSegments, "new segments")
	}

	return
}

func (s *stream) fetchPlaylist(req *http.Request) (err error) {
	s.playlist.lastDuration = s.playlist.targetDuration / 2

	resp, err := s.playlist.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = httpResponseStatusError(resp)
		return
	}

	scanner := bufio.NewScanner(resp.Body)

	if err = s.parseHeader(scanner); err != nil {
		log.Println("Failed to read playlist header:", err)
		return
	}

	if err = s.parseSegments(scanner); err != nil {
		log.Println("Failed to read playlist segments:", err)
		return
	}

	return
}

func (s *stream) playlistLoop() (err error) {
	s.playlist.client.Timeout = 5 * time.Second
	if s.d.PlaylistTimeout >= 0 {
		s.playlist.client.Timeout = s.d.PlaylistTimeout
	}

	req, err := s.d.newRequest(s.playlist.url.String())
	if err != nil {
		log.Println("Failed to create playlist request:", err)
		return
	}

	var sleep time.Duration
	for s.playlist.active {
		time.Sleep(sleep)
		if !s.playlist.active {
			break
		}

		before := time.Now()
		if err = s.fetchPlaylist(req); err != nil {
			log.Println("Failed to fetch playlist:", err)
			if _, ok := err.(fatalError); ok || s.playlist.lastDuration == 0 {
				return
			}
		}
		sleep = time.Until(before) + s.playlist.lastDuration
	}

	return
}

func (s *stream) playlistWorker() {
	defer close(s.output.queue.c)
	s.playlist.err = s.playlistLoop()
	s.playlist.active = false
}
