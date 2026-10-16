#!/bin/sh
# Builds Kalulu-<version>-x64-setup.exe, the Windows installer, and its
# SHA256SUMS. The installer carries kalulu.exe, the MinGW codec libraries it
# links, and the playback tools in tools\: yt-dlp, and ffmpeg and ffprobe
# with their libraries. Each download is checked against the checksums of
# its release.
#
# On Windows it runs in an MSYS2 MINGW64 shell with go, the mingw-w64 gcc,
# pkg-config, flac, libvorbis, libogg and mpg123, and Inno Setup 6. On
# Linux it cross-compiles: set CC=x86_64-w64-mingw32-gcc, MINGW_PREFIX to an
# extracted MSYS2 mingw64 tree with those packages, and ISCC to a command
# that runs ISCC.exe (for example "wine /path/to/ISCC.exe").
#
# Variables: VERSION (tag, default git describe), WORKDIR, MINGW_PREFIX,
# ISCC, YTDLP_VERSION (default latest), FFMPEG_BRANCH (default 8.1).
# PYTHON_VERSION and PYTHON_SHA256 pin the embeddable Python that keeps
# yt-dlp loaded between tracks; bump them together.
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
version=${VERSION:-$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)}
work=${WORKDIR:-$root/build/windows}
stage=$work/stage
mingw=${MINGW_PREFIX:-/mingw64}
ytdlp_version=${YTDLP_VERSION:-latest}
ffmpeg_branch=${FFMPEG_BRANCH:-8.1}
python_version=${PYTHON_VERSION:-3.14.8}
python_sha256=${PYTHON_SHA256:-a93abe456ab01bd96d7a085b3cdb6566b3063f4241360d114142fbdb07f0a310}

tool() {
	if command -v "x86_64-w64-mingw32-$1" >/dev/null 2>&1; then
		echo "x86_64-w64-mingw32-$1"
	else
		echo "$1"
	fi
}
windres=$(tool windres)
objdump=$(tool objdump)

# Numbers of the version for the resources: v2.6.0-3-gabc becomes 2,6,0.
nums=$(echo "${version#v}" | sed 's/[^0-9.].*//')
major=$(echo "$nums" | cut -d. -f1)
minor=$(echo "$nums" | cut -d. -f2)
patch=$(echo "$nums" | cut -d. -f3)
major=${major:-0} minor=${minor:-0} patch=${patch:-0}

rm -rf "$work"
mkdir -p "$stage/tools" "$stage/licenses" "$work/dl"

# kalulu.exe with its icon, manifest and version.
syso=$root/kalulu_windows_amd64.syso
trap 'rm -f "$syso"' EXIT
(cd "$root/packaging/windows" && "$windres" -O coff \
	-DVER_MAJOR="$major" -DVER_MINOR="$minor" -DVER_PATCH="$patch" -DVER_STRING="\\\"${version#v}\\\"" \
	-i kalulu.rc -o "$syso")
(cd "$root" && GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
	CGO_LDFLAGS="${CGO_LDFLAGS:-} -Wl,-Bstatic -logg -Wl,-Bdynamic" \
	go build -trimpath \
	-ldflags="-s -w -H windowsgui -X main.version=$version -X github.com/bjarneo/cliamp/gui.KaluluVersion=${version#v}" \
	-o "$stage/kalulu.exe" .)

# The MinGW libraries kalulu.exe needs, and theirs.
copy_dlls() {
	"$objdump" -p "$1" | awk '/DLL Name:/{print $3}' | while read -r dll; do
		if [ -f "$mingw/bin/$dll" ] && [ ! -f "$stage/$dll" ]; then
			cp "$mingw/bin/$dll" "$stage/$dll"
			copy_dlls "$stage/$dll"
		fi
	done
}
copy_dlls "$stage/kalulu.exe"

# verify FILE SUMS: FILE must be listed in SUMS with its SHA-256.
verify() {
	want=$(awk -v f="$(basename "$1")" '{n=$2; sub(/^\*/, "", n); sub(/^.*\//, "", n)} n==f {print $1}' "$2")
	got=$(sha256sum "$1" | cut -d' ' -f1)
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		echo "checksum mismatch for $1" >&2
		exit 1
	fi
}

# yt-dlp, the self-updating build.
if [ "$ytdlp_version" = latest ]; then
	ytdlp_url=https://github.com/yt-dlp/yt-dlp/releases/latest/download
else
	ytdlp_url=https://github.com/yt-dlp/yt-dlp/releases/download/$ytdlp_version
fi
curl -fsSL -o "$work/dl/yt-dlp.exe" "$ytdlp_url/yt-dlp.exe"
curl -fsSL -o "$work/dl/SHA2-256SUMS" "$ytdlp_url/SHA2-256SUMS"
verify "$work/dl/yt-dlp.exe" "$work/dl/SHA2-256SUMS"
cp "$work/dl/yt-dlp.exe" "$stage/tools/"

# The yt-dlp zipapp and the embeddable Python that runs it. yt-dlp.exe is
# frozen and starts for every call; Kalulu keeps the zipapp loaded instead.
curl -fsSL -o "$work/dl/yt-dlp" "$ytdlp_url/yt-dlp"
verify "$work/dl/yt-dlp" "$work/dl/SHA2-256SUMS"
cp "$work/dl/yt-dlp" "$stage/tools/yt-dlp.pyz"
python_zip=python-$python_version-embed-amd64.zip
curl -fsSL -o "$work/dl/$python_zip" "https://www.python.org/ftp/python/$python_version/$python_zip"
if [ "$(sha256sum "$work/dl/$python_zip" | cut -d' ' -f1)" != "$python_sha256" ]; then
	echo "checksum mismatch for $python_zip" >&2
	exit 1
fi
mkdir -p "$stage/tools/python"
(cd "$stage/tools/python" && unzip -q "$work/dl/$python_zip")

# ffmpeg and ffprobe of the BtbN builds, GPL, with shared libraries.
ffmpeg_zip=ffmpeg-n$ffmpeg_branch-latest-win64-gpl-shared-$ffmpeg_branch.zip
ffmpeg_url=https://github.com/BtbN/FFmpeg-Builds/releases/download/latest
curl -fsSL -o "$work/dl/$ffmpeg_zip" "$ffmpeg_url/$ffmpeg_zip"
curl -fsSL -o "$work/dl/checksums.sha256" "$ffmpeg_url/checksums.sha256"
verify "$work/dl/$ffmpeg_zip" "$work/dl/checksums.sha256"
(cd "$work/dl" && unzip -q "$ffmpeg_zip")
ffdir=$work/dl/${ffmpeg_zip%.zip}
cp "$ffdir/bin/ffmpeg.exe" "$ffdir/bin/ffprobe.exe" "$ffdir"/bin/*.dll "$stage/tools/"
cp "$ffdir/LICENSE.txt" "$stage/licenses/FFmpeg-LICENSE.txt"

# Licenses.
cp "$root/LICENSE" "$stage/licenses/LICENSE.txt"
cp "$root/LICENSES/MIT-cliamp.txt" "$stage/licenses/"
ytdlp_built=$("$work/dl/yt-dlp.exe" --version 2>/dev/null || echo "$ytdlp_version")
cat >"$stage/licenses/THIRD-PARTY.txt" <<EOF
Kalulu $version for Windows includes these programs, unmodified:

yt-dlp $ytdlp_built (yt-dlp.exe and the zipapp yt-dlp.pyz)
  The Unlicense (public domain). https://github.com/yt-dlp/yt-dlp

Python $python_version, embeddable package (tools\python)
  Python Software Foundation License. https://www.python.org
  https://docs.python.org/3/license.html

FFmpeg $ffmpeg_branch (ffmpeg, ffprobe and their libraries)
  GPL version 3 or later, see FFmpeg-LICENSE.txt.
  Build: https://github.com/BtbN/FFmpeg-Builds ($ffmpeg_zip)
  Source: https://ffmpeg.org/download.html and https://github.com/BtbN/FFmpeg-Builds

libFLAC, libogg, libvorbis, mpg123 and the MinGW runtime
  From the MSYS2 project (https://packages.msys2.org); BSD-style and LGPL
  licenses, sources at https://github.com/msys2/MINGW-packages
EOF

# The installer.
cp "$root/packaging/windows/kalulu.iss" "$root/packaging/windows/kalulu.ico" "$work/"
cat >"$work/version.iss" <<EOF
#define AppVersion "${version#v}"
#define AppTag "$version"
#define NumVersion "$major.$minor.$patch.0"
EOF
# MSYS2 must not rewrite /Q into a path for ISCC.
(cd "$work" && MSYS2_ARG_CONV_EXCL='*' ${ISCC:-iscc} /Q kalulu.iss)
setup=Kalulu-$version-x64-setup.exe
test -f "$work/$setup"
(cd "$work" && sha256sum "$setup" >SHA256SUMS)
echo "built $work/$setup and $work/SHA256SUMS"
