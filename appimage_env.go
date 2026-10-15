package main

import "os"

// The AppImage launcher puts its bundled codec libraries first in
// LD_LIBRARY_PATH so Kalulu finds them. Children such as ffmpeg and yt-dlp
// must not inherit that path: the host ffmpeg would load the older bundled
// libraries and stop with a symbol error, and no track would play. The
// libraries of Kalulu are loaded by now, so the host's path comes back for
// every process Kalulu starts.
func init() {
	if os.Getenv("KALULU_APPIMAGE_LIBS") != "1" {
		return
	}
	if host := os.Getenv("KALULU_HOST_LD_LIBRARY_PATH"); host != "" {
		_ = os.Setenv("LD_LIBRARY_PATH", host)
	} else {
		_ = os.Unsetenv("LD_LIBRARY_PATH")
	}
	_ = os.Unsetenv("KALULU_HOST_LD_LIBRARY_PATH")
	_ = os.Unsetenv("KALULU_APPIMAGE_LIBS")
}
