// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package hls

import (
	"errors"
	"log"
	"sync"
	"time"
)

type stream struct {
	d        *Dumper
	name     string
	playlist playlist
	output   output
}

type Dumper struct {
	URL        string
	Name       string
	SingleFile bool
	Verbose    bool
	Headers    map[string][]string
	Groups     []string
	Titles     []string

	PlaylistTimeout time.Duration
	SegmentTimeout  int

	streams []*stream
	stop    bool
}

var errNoStreamsFound = errors.New("no streams found")

func (s *stream) dump() (err error) {
	s.playlist.active = true
	s.output.queue.c = make(chan *segment, 64)
	s.output.queue.sequence = -1

	if s.playlist.file, err = createFileWriteOnly(s.name + ".m3u8"); err != nil {
		log.Println("Failed to create playlist file", err)
		return
	}
	defer s.playlist.file.Close()

	go s.playlistWorker()
	err = s.downloadWorker()

	if s.playlist.writer != nil {
		defer s.playlist.flush(&err)
		if err = writeLine(s.playlist.writer, "#EXT-X-ENDLIST"); err != nil {
			log.Println("Failed to write #EXT-X-ENDLIST:", err)
		}
	}
	if err == nil {
		err = s.playlist.err
	}
	return
}

func (s *stream) start(wg *sync.WaitGroup, erro *error) {
	defer wg.Done()
	if err := s.dump(); *erro == nil {
		*erro = err
	}
}

func (d *Dumper) startAll() (err error) {
	var wg sync.WaitGroup
	wg.Add(len(d.streams))
	for _, s := range d.streams {
		go s.start(&wg, &err)
	}
	wg.Wait()
	return
}

func (d *Dumper) Start() (err error) {
	if err = d.loadMaster(); err != nil {
		log.Println("Failed to load master playlist", err)
		return
	}

	switch len(d.streams) {
	case 0:
		err = errNoStreamsFound
		log.Println(err)
	case 1:
		// Start directly
		err = d.streams[0].dump()
	default:
		err = d.startAll()
	}

	return
}

func (d *Dumper) Stop() {
	for _, s := range d.streams {
		s.playlist.active = false
	}
	d.stop = true
}
