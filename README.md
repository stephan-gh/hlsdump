# hlsdump
hlsdump is a simple command line tool that allows dumping [HTTP Live Streaming (HLS)] streams (live or VOD)
as-is with no transformations like transcoding or repacking to another video/audio container format. This can
be used to fully dump a HLS stream for later investigation.

**Note:** hlsdump does not know anything about particular video/audio containers or codecs, it just understands
the HLS playlist format and downloads all segments it can find.

## Building
hlsdump is written in Go and can be built using `go build`. Then, simply run `./hlsdump` (or `hlsdump.exe` on Windows).

Check `./hlsdump -help` to see all available command line options.

## Example
Apple (the original developer of HLS) provides some example streams at: https://developer.apple.com/streaming/examples/

To dump the ["basic stream"](https://developer.apple.com/streaming/examples/basic-stream-osx-ios5.html)
using hlsdump, you would use `./hlsdump https://devstreaming-cdn.apple.com/videos/streaming/examples/bipbop_16x9/bipbop_16x9_variant.m3u8`.
Note that this will create many many small files in the current directory since each segment is stored in a separate file.

To avoid that, the one and only transformation that hlsdump supports is to use the `EXT-X-BYTERANGE` feature
to store all segments in a single file. You can enable that using the `-single-file` parameter.

hlsdump will then download the master playlist and all available streams (different video resolutions, audio qualities, ...).
The downloaded stream playlists `.m3u8` next to a audio/video file can be typically also played with players like [VLC] or [mpv],
or further transformed using [ffmpeg].

**Note:** The master playlist is also dumped to the current directory, but currently it is not transformed to point to the downloaded
stream playlists. It can be easily edited with a text editor to point there, though.

[VLC]: https://www.videolan.org/vlc/
[mpv]: https://mpv.io/
[ffmpeg]: https://ffmpeg.org/
[HTTP Live Streaming (HLS)]: https://en.wikipedia.org/wiki/HTTP_Live_Streaming
