// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package main

import (
	"flag"
	"fmt"
	"hlsdump/hls"
	"log"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
)

type listFlag []string

func (l *listFlag) String() string {
	return strings.Join(*l, "\n")
}

func (l *listFlag) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s <url.m3u8>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func parse() *hls.Dumper {
	var name string
	flag.StringVar(&name, "name", "", "Output file name prefix (without file extension)")
	singleFile := flag.Bool("single-file", false, "Store segments in single file (using EXT-X-BYTERANGE)")
	verbose := flag.Bool("verbose", false, "Verbose output")

	var headers listFlag
	flag.Var(&headers, "header", "Additional HTTP headers to use for HTTP(s) requests")

	var groups listFlag
	flag.Var(&groups, "group", "Only download streams that use the specified rendition group IDs")

	var titles listFlag
	flag.Var(&titles, "title", "Only download segments with specified title")

	playlistTimeout := flag.Duration("playlist-timeout", -1, "Timeout for playlist download")
	segmentTimeout := flag.Int("segment-timeout", -1, "Timeout multiplier for segment download")

	flag.Usage = usage
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
	}

	url := flag.Arg(0)
	if name == "" {
		name = path.Base(url)
	}

	h, err := hls.ParseHeaders(headers)
	if err != nil {
		fmt.Fprintln(flag.CommandLine.Output(), err)
		os.Exit(2)
	}

	return &hls.Dumper{
		URL:        flag.Arg(0),
		Name:       name,
		SingleFile: *singleFile,
		Verbose:    *verbose,
		Headers:    h,
		Groups:     groups,
		Titles:     titles,

		PlaylistTimeout: *playlistTimeout,
		SegmentTimeout:  *segmentTimeout,
	}
}

func signalHandler(d *hls.Dumper) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	for sig := range c {
		log.Println("Received signal:", sig)
		d.Stop()
	}
}

func main() {
	d := parse()

	go signalHandler(d)
	err := d.Start()
	if err != nil {
		os.Exit(1)
	}
}
