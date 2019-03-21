// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package hls

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var mediaTags = map[string]struct{}{
	"EXT-X-TARGETDURATION":         {},
	"EXT-X-MEDIA-SEQUENCE":         {},
	"EXT-X-DISCONTINUITY-SEQUENCE": {},
	"EXT-X-PLAYLIST-TYPE":          {},
	"EXT-X-I-FRAMES-ONLY":          {},
}

var (
	attributeListPattern    = regexp.MustCompile(`([A-Z0-9-]+)=([^",]+|"[^"]*")(?:,|$)`)
	errInvalidAttributeList = errors.New("invalid attribute list")
	errMixedPlaylist        = errors.New("mixed master/media playlist")
)

func parseAttributeList(value string) map[string]string {
	matches := attributeListPattern.FindAllStringSubmatch(value, -1)
	if matches == nil {
		return nil
	}

	attr := make(map[string]string, len(matches))
	for _, match := range matches {
		v := match[2]
		l := len(v)
		// Strip quotes
		if l >= 2 && v[0] == '"' && v[l-1] == '"' {
			v = v[1 : l-1]
		}
		attr[match[1]] = v
	}
	return attr
}

func (d *Dumper) matchRenditions(attr map[string]string) bool {
	if len(d.Groups) == 0 {
		return true
	}

	return contains(d.Groups, attr["VIDEO"]) || contains(d.Groups, attr["AUDIO"]) ||
		contains(d.Groups, attr["SUBTITLES"]) || contains(d.Groups, attr["CLOSED-CAPTIONS"])
}

func (d *Dumper) parseMaster(masterURL *url.URL, scanner *bufio.Scanner) (err error) {
	if !scanner.Scan() {
		err = eofIfNil(scanner.Err())
		return
	}

	line := scanner.Text()
	if line != "#EXTM3U" {
		err = errors.New("playlist file does not start with #EXTM3U: " + line)
		return
	}

	matchedStream := false
	i := 0

	for scanner.Scan() {
		line = scanner.Text()
		if line == "" {
			continue
		}

		if line[0] == '#' {
			if !strings.HasPrefix(line, tagPrefix) {
				continue
			}

			k, v := splitPair(line[1:], tagSeparator)
			switch k {
			case "EXT-X-MEDIA":
				attr := parseAttributeList(v)
				if attr == nil {
					err = errInvalidAttributeList
					break
				}

				// TODO: Support renditions
			case "EXT-X-STREAM-INF":
				attr := parseAttributeList(v)
				if attr == nil {
					err = errInvalidAttributeList
					break
				}

				if d.matchRenditions(attr) {
					matchedStream = true
				}
			default:
				_, media := mediaTags[k]
				_, segment := segmentTags[k]
				if media || segment {
					if len(d.streams) > 0 {
						err = errMixedPlaylist
						return
					}

					s := &stream{
						d:    d,
						name: d.Name,
					}
					s.playlist.url = masterURL
					d.streams = []*stream{s}
					return
				}
			}

			if err != nil {
				err = fmt.Errorf("invalid %s tag with value '%s': %s", k, v, err)
				return
			}

			continue
		}

		if matchedStream {
			i++
			matchedStream = false

			s := &stream{
				d:    d,
				name: fmt.Sprintf("%s-%d", d.Name, i),
			}
			s.playlist.url, err = masterURL.Parse(line)
			if err != nil {
				return
			}

			log.Println("Downloading stream:", s.playlist.url)
			d.streams = append(d.streams, s)
		}
	}

	return
}

func (d *Dumper) fetchMaster() (masterURL *url.URL, b []byte, err error) {
	req, err := d.newRequest(d.URL)
	if err != nil {
		return
	}
	masterURL = req.URL

	client := http.Client{
		Timeout: d.PlaylistTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = httpResponseStatusError(resp)
		return
	}

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	// TODO: Replace names in playlist
	f, err := createFileWriteOnly(d.Name + ".m3u8")
	if err != nil {
		return
	}
	defer f.Close()
	_, err = f.Write(b)
	return
}

func (d *Dumper) loadMaster() (err error) {
	masterURL, b, err := d.fetchMaster()
	if err != nil {
		return
	}

	err = d.parseMaster(masterURL, bufio.NewScanner(bytes.NewReader(b)))
	if err != nil {
		return
	}
	return
}
