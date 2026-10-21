# Kivo

Kivo is the third server type, after Plex and Jellyfin. Its catalog maps onto
kino's columns directly, but its playback doesn't fit the pull-a-URL model the
other two use. This note records how kino fits Kivo and what was checked
against a real server.

## Playback is a session, not a URL

A Kivo play is something the server keeps:

- `POST /plays` takes the work, a position and the client's capabilities. It
  returns the play's ID and its media (`file` parts with range requests, or an
  `hls` playlist), relative to `/api/v1/`. The play ID in those URLs is their
  credential, so the player needs no auth header.
- `PATCH /plays/{id}` is the heartbeat: every 10 s, and on pause, seek and end.
- A play with no heartbeat **and** no media request for 2 minutes expires, and
  its media URLs stop working. A player paused past that on a buffered file
  makes no requests, so without heartbeats pausing breaks playback, and every
  resume point is lost.
- `POST /plays/{id}/stop` saves the position. `POST /plays/{id}/fallback`
  retries a play the player couldn't open as a full transcode, once.

### How kino follows a play

`player.Starter.StartPlay` returns a `media.Stream`: the parts, the start
position and, for a server that keeps plays, a `PlayID`. Plex and Jellyfin
return a single URL with no ID and launch as before. A stream with an ID goes to
`kino play-session`, a hidden subcommand. It takes the stream as JSON on stdin
and lives as long as the player, whether or not the TUI is still running. It:

1. launches the player, giving mpv, IINA and Celluloid an IPC socket
   (`--input-ipc-server`) in a directory whose socket paths fit the unix limit;
2. watches `time-pos`, `pause` and `playlist-pos`, plus the `seek`,
   `playback-restart` and `end-file` events. It reports at the first
   position, every 10 s, on pause and resume, after a seek (with the time to
   its first frame), and at the end;
3. on `end-file` with an error before any position, asks for one fallback
   with mpv's `file_error` and relaunches on the new play;
4. on the player's exit or a signal, stops the play with the last position.
   A play the server ended (a heartbeat `404`: expired, or stopped by an
   admin) is left alone.

The TUI waits up to 10 s for the session to say the play is **playing** or has
failed, so a refusal such as a failed fallback shows in the footer instead of
"Launched". After 10 s it stops waiting. It never kills the session, because
that would orphan a running player.

A player without IPC (VLC, PotPlayer, the system handler) still gets a play
kept alive by heartbeats that repeat the start position, and is stopped there,
so the resume point stays where it was. A launch that can't be followed at all
(`open -a`, a URL handler) is left to expire on its own.

The first mpv socket ran 114 bytes in a long runtime directory, and mpv failed
to create it. The session now picks the first of `XDG_RUNTIME_DIR`, the temp
dir and `/tmp` that fits.

### What kino declares

Containers `mkv mp4 webm avi ts mov`, HLS, video `h264 hevc av1 vp9` up to
10-bit with HDR10 and HLG, audio `aac ac3 eac3 opus flac mp3 truehd dts`, and
subtitles `vtt ass pgs`. Dolby Vision is left out, because how a DV-only stream
looks depends on the player's video output, so the server decides. A file the
player still can't open takes the fallback path.

Multi-part `file` plays start at the part holding the position, using mpv's
per-file options (`--{ --start=S part --}`). Heartbeats are on the joined
timeline. This is covered by tests; there was no multi-part video on the test
server.

## Catalog

| kino | Kivo |
| --- | --- |
| libraries | `GET /libraries`: `movies` and `shows`. `audiobooks` and `videos` (folders of loose videos) have no columns in kino and are left out. |
| movies, shows | `GET /works?libraryId=&offset=&limit=`. The total comes on the first page only, so the client keeps it for later pages. |
| seasons, episodes | `GET /works?parentId=` |
| item count | `Library.itemCount` |
| mark played, unplayed | `PATCH /works/{id}/user-state {played}` |

A series card carries its unplayed count but not its episode or season count.
`Show.EpisodeCount` stays zero, meaning unknown. The watch roll-up no longer
clamps the unwatched count to a zero episode count, which made a show look
watched after one episode, and the inspector shows "Unwatched episodes: N"
for such a show. If Kivo adds `episodeCount` to the series card, the full
progress line comes back with no kino change.

Kivo has no playlists. The playlist methods moved out of `catalog.Backend` into
`catalog.PlaylistBackend`. For a backend without them, the catalog returns
`ErrNoPlaylists`, and the TUI drops the Playlists row and the playlist modal.

## Sign-in

`GET /api/v1/server` (signed out) detects Kivo. It is probed before Jellyfin
and Plex because it is the most specific check. Sign-in uses a device code:
`POST /device-codes {name: "kino on <host>", kind: app, clientId: device_id}`,
with no platform. Kivo made `platform` optional for this (Kivo `3b6d0aec`).
kino prints the `/link` URL and code, polls at the given interval, and reads
`/users/me` for the user ID and name. A denied code (`403`) or an expired one
(`410`) ends the setup with that reason.

## Errors

Problem codes become `media.RefusedError`, whose text the TUI shows as it is:

| Server | User sees |
| --- | --- |
| `409 streamLimit` | Already playing on Living Room (Dune) and iPhone (Severance). Stop one to play here. |
| `503 transcodeBusy` | The server is busy transcoding. Try again in a minute. / The server's transcode disk is full. |
| `409 notPlayable` | Can't play this here: *reason in words*. |
| `409 noFallback` | playback failed and the server has nothing else to try |

Play URLs are redacted in logged player arguments, and the session doesn't
log play IDs.

## How it was checked

- **Fixtures** in `internal/mediaserver/kivo/testdata` were recorded from a
  real server (Kivo `a7edf8d3`), trimmed and scrubbed. The streamLimit,
  transcodeBusy, noFallback and 401 problem documents and the approved
  session were written from the spec.
- **Contract:** the tests' fake server checks every request the client sends,
  and every fixture it replays, against Kivo's `openapi.yaml` using
  kin-openapi. The Kivo repository is private, so the spec is read from a
  sibling checkout (`../kivo`, or `$KIVO_OPENAPI`) and the checks are
  skipped without it. A planted bad heartbeat state was caught.
- **Lifecycle:** session tests run a scripted fake mpv on a real unix socket
  with a controlled clock. They cover the normal run, connecting after the
  first frame, pause, seek timing, end, one fallback then a stop, no fallback
  left, a play the server ended, a signal, multi-part positions, a blind
  player and a failed launch.
- **On the live server**, with mpv 0.41:
  - setup detected Kivo and approved the device code;
  - the TUI listed only the video libraries;
  - resuming an episode showed `kino on <host>` in Now playing, with the
    position advancing every 10 s;
  - a 2.5-minute pause kept the play alive;
  - a seek's time to first frame reached the play's diagnostics;
  - quitting mpv ended the play with mpv's last position as the resume
    point, and kino resumed there when restarted;
  - a movie whose file is missing on disk failed in mpv (HTTP 500), took one
    fallback, and its refusal reached the TUI's footer.

## Known gaps

- After a play, the TUI shows the resume point from its cached list until the
  list revalidates (restart, or `r`). Resuming again before that starts from
  the older point. Plex and Jellyfin have the same gap, but there the server
  never learned the new position anyway.
- Subtitle files Kivo lists beside the media (`subtitles`) aren't passed to the
  player. Direct-play files carry their own, and HLS streams carry the ones
  marked `stream`.
- IPC is unix-socket only. Windows players, including a Windows mpv under WSL,
  run blind.
