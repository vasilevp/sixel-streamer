#!/usr/bin/env bash
set -euo pipefail

export repo=$PWD

# Default values
PORT=9000
DISPLAY=:99
RESOLUTION="320x240"
COLORS=32
DIFFUSE=none
DROPFRAME=1
IGNOREDELAY=1
SIXEL_ENCODER="sixel_vfr"

# Parse named arguments
while getopts "p:d:r:c:D:F:I:S:h" opt; do
	case $opt in
	p) PORT="$OPTARG" ;;
	d) DISPLAY="$OPTARG" ;;
	r) RESOLUTION="$OPTARG" ;;
	c) COLORS="$OPTARG" ;;
	D) DIFFUSE="$OPTARG" ;;
	F) DROPFRAME="$OPTARG" ;;
	I) IGNOREDELAY="$OPTARG" ;;
	S)
		if [[ "$OPTARG" != "sixel" && "$OPTARG" != "sixel_vfr" ]]; then
			echo "Error: -S must be either 'sixel' or 'sixel_vfr'" >&2
			exit 1
		fi
		SIXEL_ENCODER="$OPTARG"
		;;
	h)
		echo "Usage: $0 [-p port] [-d display] [-r resolution] [-c colors] [-D diffuse] [-F dropframe] [-I ignoredelay] [-S sixel_encoder]"
		echo "  -p PORT          TCP port for telfwd (default: 9000)"
		echo "  -d DISPLAY       X11 display (default: :99)"
		echo "  -r RESOLUTION    Video resolution (default: 320x240)"
		echo "  -c COLORS        Number of colors (default: 32)"
		echo "  -D DIFFUSE       Diffuse setting (default: none)"
		echo "  -F DROPFRAME     Dropframe setting (default: 1)"
		echo "  -I IGNOREDELAY   Ignore delay setting (default: 1)"
		echo "  -S SIXEL_ENCODER Sixel encoder: 'sixel' or 'sixel_vfr' (default: sixel_vfr)"
		echo "  -h               Show this help"
		exit 0
		;;
	\?)
		echo "Invalid option: -$OPTARG" >&2
		echo "Use -h for help" >&2
		exit 1
		;;
	:)
		echo "Option -$OPTARG requires an argument" >&2
		exit 1
		;;
	esac
done

findscreen() {
	screen -ls "$1" | grep -q "$1"
}

# Use a function instead of alias for complex commands
ffmpeg() {
	local a="$repo/ffmpeg"
	if [ ! -f "$a/ffmpeg" ]; then
		echo "ffmpeg binary not found: $a" >&2
		echo "Building with default configuration..." >&2

		pushd $a
		if [ ! -f "$a/config.h" ]; then
			set -x
			./configure --enable-libsixel --disable-doc --enable-libxcb
			set +x
		fi
		make -j$(nproc)
		popd
	fi

	env LD_LIBRARY_PATH="$a/libavfilter:$a/libavdevice:$a/libswresample:$a/libavcodec:$a/libswscale:$a/libpostproc:$a/libavutil:$a/libavformat:${LD_LIBRARY_PATH:-}" "$a/ffmpeg" "$@"
}

# Export the function so it's available in subshells
export -f ffmpeg

export DISPLAY=:99

if findscreen xvfb; then
	echo "Xvfb screen already running"
else
	echo "Starting Xvfb on display $DISPLAY"
	screen -dm -S xvfb Xvfb $DISPLAY -screen 0 480x360x24 +extension RANDR
	sleep 1
fi

if findscreen vnc; then
	echo "x11vnc server already running"
else
	echo "Starting x11vnc server"
	screen -dm -S vnc x11vnc -display $DISPLAY -nopw -listen server -xkb -ncache 10 -forever
	sleep 1
fi

if findscreen ffmpeg; then
	echo "ffmpeg/telfwd pipeline already running"
else
	echo "Starting ffmpeg/telfwd pipeline to forward ffmpeg output to clients"
	screen -dm -S ffmpeg bash -c "
		pushd $repo/telfwd
		ffmpeg -f x11grab -i $DISPLAY -vf "drawtext=textfile='overlay.txt':x=300:y=0:fontcolor=white@0.5:fontsize=12:reload=1" -f $SIXEL_ENCODER -s $RESOLUTION -diffuse $DIFFUSE -reqcolors $COLORS -ignoredelay $IGNOREDELAY -dropframe $DROPFRAME - | go run . $PORT | ./sendkey.sh
		popd
	"
fi

echo "All services started."
