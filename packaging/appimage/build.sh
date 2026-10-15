#!/bin/sh
# Builds Kalulu-<arch>.AppImage. It bundles the audio codec libraries, whose
# sonames differ between distributions (libFLAC.so.12 on Debian 12,
# libFLAC.so.14 on newer ones), and leaves libGL, X11, Wayland and ALSA to the
# host, as every desktop distribution ships them and they must match its
# drivers.
#
# Needs: go, gcc, the build headers listed in docs/gui.md, and appimagetool
# (https://github.com/AppImage/appimagetool) in PATH or in $APPIMAGETOOL.
# Build on the oldest distribution you want to support: glibc is not bundled.
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
arch=$(uname -m)
version=${VERSION:-$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)}
work=${WORKDIR:-$root/build/appimage}
appdir=$work/Kalulu.AppDir
tool=${APPIMAGETOOL:-appimagetool}

rm -rf "$appdir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/lib" \
	"$appdir/usr/share/applications" "$appdir/usr/share/icons/hicolor/512x512/apps" \
	"$appdir/usr/share/metainfo"

(cd "$root" && go build -trimpath -ldflags="-s -w -X main.version=$version -X github.com/bjarneo/cliamp/gui.KaluluVersion=${version#v}" -o "$appdir/usr/bin/kalulu" .)

# Bundle the codec libraries the binary links, and their own codec deps.
bundle() {
	ldd "$1" | awk '/=> \//{print $1" "$3}' | while read -r name path; do
		case "$name" in
		libFLAC.so* | libmpg123.so* | libvorbis.so* | libvorbisenc.so* | libvorbisfile.so* | libogg.so* | libopus.so* | libout123.so*)
			if [ ! -e "$appdir/usr/lib/$name" ]; then
				cp -L "$path" "$appdir/usr/lib/$name"
			fi
			;;
		esac
	done
}
bundle "$appdir/usr/bin/kalulu"
for lib in "$appdir"/usr/lib/*.so*; do
	[ -e "$lib" ] && bundle "$lib"
done

cp "$root/packaging/kalulu.desktop" "$appdir/usr/share/applications/kalulu.desktop"
cp "$root/packaging/kalulu.desktop" "$appdir/kalulu.desktop"
cp "$root/packaging/kalulu.png" "$appdir/usr/share/icons/hicolor/512x512/apps/kalulu.png"
cp "$root/packaging/kalulu.png" "$appdir/kalulu.png"
cp "$root/packaging/io.kalulu.Kalulu.metainfo.xml" "$appdir/usr/share/metainfo/"

cat >"$appdir/AppRun" <<'RUN'
#!/bin/sh
here=$(dirname "$(readlink -f "$0")")
# Kalulu gives the host path back to ffmpeg and yt-dlp (appimage_env.go).
export KALULU_HOST_LD_LIBRARY_PATH="${LD_LIBRARY_PATH:-}"
export KALULU_APPIMAGE_LIBS=1
export LD_LIBRARY_PATH="$here/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$here/usr/bin/kalulu" "$@"
RUN
chmod +x "$appdir/AppRun"

ARCH=$arch "$tool" "$appdir" "$work/Kalulu-$version-$arch.AppImage"
# The in-app updater installs only a download whose SHA-256 matches this
# list, so upload SHA256SUMS with the AppImage to the GitHub release.
(cd "$work" && sha256sum Kalulu-*.AppImage >SHA256SUMS)
echo "built $work/Kalulu-$version-$arch.AppImage and $work/SHA256SUMS"
