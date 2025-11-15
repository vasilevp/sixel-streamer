# SIXEL Streaming Server

A real-time SIXEL graphics streaming server that broadcasts X11 screen content to multiple dumb clients simultaneously. This was initially created to stream DOOM through a terminal.

## Overview

This project combines two main components:
1. **FFmpeg with SIXEL support** - Custom FFmpeg build with SIXEL encoder for efficient terminal graphics
2. **telfwd** - Go-based TCP server that broadcasts SIXEL frames and handles client input

## Features

- 🎮 **Real-time multiplayer** - Multiple clients can connect and interact with the same session
- 🖼️ **SIXEL graphics** - Efficient terminal graphics with configurable colors and quality
- ⚡ **Optimized streaming** - Frame dropping, duplicate detection, and scene change detection
- 🔄 **Graceful shutdown** - Proper connection handling and cleanup
- 📊 **Structured logging** - Clear logging with zerolog showing function names and context
- 🎨 **Customizable** - Adjustable resolution, colors, and encoding parameters

## Architecture

```
┌─────────────┐
│ crispy-doom │ (or any X11 app)
└──────┬──────┘
       │
       ↓ (X11 display :99)
┌─────────────┐
│    Xvfb     │ Virtual framebuffer
└──────┬──────┘
       │
       ↓ (x11grab)
┌─────────────┐
│   FFmpeg    │ with SIXEL encoder
└──────┬──────┘
       │
       ↓ (SIXEL frames via pipe)
┌─────────────┐
│   telfwd    │ TCP broadcast server
└──────┬──────┘
       │
       ↓ (TCP :9000)
┌─────────────┐
│   Clients   │ nc/socat connections
└─────────────┘
       │
       ↓ (input commands)
┌─────────────┐
│  sendkey.sh │ xdotool keyboard input
└─────────────┘
```

## Prerequisites

### System Packages

#### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install -y \
    build-essential \
    git \
    screen \
    xvfb \
    xdotool \
    yasm \
    nasm \
    libxcb1-dev \
    libxcb-shm0-dev \
    libxcb-xfixes0-dev \
    libsixel-dev \
    golang-go
```

#### Arch Linux
```bash
sudo pacman -S --needed \
    base-devel \
    git \
    screen \
    xorg-server-xvfb \
    xdotool \
    yasm \
    nasm \
    libxcb \
    libsixel \
    go
```

**Core Dependencies Explained:**
- **build-essential/base-devel** - Compiler toolchain (gcc, make, etc.)
- **git** - Version control for cloning repository
- **screen** - Terminal multiplexer for managing services
- **xvfb** - Virtual X11 framebuffer (headless X server)
- **xdotool** - Simulate keyboard input from client connections
- **yasm/nasm** - Assembly optimizations for FFmpeg
- **libxcb*** - X11 screen capture support (x11grab)
- **libsixel** - SIXEL graphics encoding
- **go/golang-go** - Go compiler for telfwd server

**Optional Dependencies:**
- **x11vnc** - For remote viewing/debugging the X11 session (not required for streaming)

### Go

Go 1.22.2 or later is required for the telfwd server.

## Installation

### 1. Clone the Repository

```bash
git clone --recursive git@github.com:vasilevp/sixel-streamer.git
cd sixel-streamer
```

If you already cloned without `--recursive`:
```bash
git submodule update --init --recursive
```

### 2. Build FFmpeg with SIXEL Support

The custom FFmpeg build includes:
- SIXEL output device with two encoders: `sixel` and `sixel_vfr`
- Optimized duplicate frame detection
- Scene change detection for adaptive frame dropping
- X11 screen capture (x11grab)

```bash
cd ffmpeg
./configure \
    --enable-libsixel \
    --enable-libxcb \
    --disable-doc
make -j$(nproc)
cd ..
```

**Note:** The `start.bash` script will automatically build FFmpeg if not already compiled.

### 3. Build the telfwd Server

```bash
cd telfwd
go build
cd ..
```

## Usage

### Quick Start

Start all services with default settings:

```bash
./start.bash
```

Connect from a client:
```bash
telnet <server-ip> 9000
```

Or using netcat:
```bash
nc <server-ip> 9000
```

### Command-Line Options

```bash
./start.bash [OPTIONS]

Options:
  -p PORT          TCP port for telfwd (default: 9000)
  -d DISPLAY       X11 display (default: :99)
  -r RESOLUTION    Video resolution (default: 320x240)
  -c COLORS        Number of colors (default: 32)
  -D DIFFUSE       Diffuse setting for dithering (default: none)
                   Options: none, atkinson, fs, jajuni, stucki, burkes
  -F DROPFRAME     Enable frame dropping (default: 1)
                   0 = disabled, 1 = enabled
  -I IGNOREDELAY   Ignore delay setting (default: 1)
                   0 = respect timing, 1 = stream as fast as possible
  -S SIXEL_ENCODER SIXEL encoder type (default: sixel_vfr)
                   sixel     = fixed framerate
                   sixel_vfr = variable framerate with duplicate detection
  -h               Show help message
```

### Example Configurations

**High quality, more colors (slower):**
```bash
./start.bash -r 640x480 -c 256 -D fs
```

**Fast streaming, fewer colors:**
```bash
./start.bash -r 320x200 -c 16 -D none
```

**Custom port:**
```bash
./start.bash -p 8080
```

**Fixed framerate encoding:**
```bash
./start.bash -S sixel -I 0
```

## SIXEL Encoder Comparison

### `sixel` - Fixed Framerate Encoder
- Standard SIXEL output with consistent timing
- Use with `-I 0` to respect frame timing
- Better for pre-recorded content
- Simpler, more predictable behavior

### `sixel_vfr` - Variable Framerate Encoder (Recommended)
- **Duplicate frame detection** - Skips identical frames to save bandwidth
- **Scene change detection** - Adapts to content changes
- **Statistics output** - Shows rendered/dropped/skipped frame counts
- Better for real-time streaming
- More efficient bandwidth usage

## Client Controls

When connected to Terminal Doom:

- **Movement:** WASD
- **Turn left/right:** Q/E
- **Fire:** F
- **Use:** SPACE
- **Sprint:** SHIFT (CAPSLOCK to toggle)
- **Arrow keys:** Also supported for navigation

**Important:** Tap keys repeatedly instead of holding them down to prevent input buffering.

## Services Management

The `start.bash` script manages three screen sessions:

1. **xvfb** - Virtual X11 framebuffer
2. **vnc** - x11vnc server (for remote viewing/debugging)
3. **ffmpeg** - FFmpeg + telfwd + sendkey.sh pipeline

### View Running Services

```bash
screen -ls
```

### Attach to a Service

```bash
# Attach to FFmpeg output
screen -r ffmpeg

# Attach to VNC server
screen -r vnc

# Attach to Xvfb
screen -r xvfb
```

Press `Ctrl+A` then `D` to detach from a screen session.

### Stop Services

```bash
# Kill specific service
screen -S ffmpeg -X quit
screen -S vnc -X quit
screen -S xvfb -X quit

# Or kill all at once
pkill -f "screen -dm"
```

## Manual Operation

For development or debugging, you can run components individually:

### 1. Start Xvfb
```bash
Xvfb :99 -screen 0 800x600x24 +extension RANDR &
```

### 2. Start x11vnc (optional, for debugging)
```bash
x11vnc -display :99 -nopw -listen localhost -xkb -forever &
```

### 3. Run your application
```bash
DISPLAY=:99 crispy-doom  # or any X11 application
```

### 4. Start the streaming pipeline
```bash
cd telfwd
export LD_LIBRARY_PATH="../ffmpeg/libavfilter:../ffmpeg/libavdevice:../ffmpeg/libswresample:../ffmpeg/libavcodec:../ffmpeg/libswscale:../ffmpeg/libpostproc:../ffmpeg/libavutil:../ffmpeg/libavformat"

../ffmpeg/ffmpeg \
    -f x11grab \
    -i :99 \
    -f sixel_vfr \
    -s 320x240 \
    -diffuse none \
    -reqcolors 32 \
    -ignoredelay 1 \
    -dropframe 1 \
    - | ./telfwd 9000 | ./sendkey.sh
```

## Performance Tuning

### Network Bandwidth
- Reduce `-c` (colors) for lower bandwidth: 16-32 colors work well
- Lower `-r` (resolution): 320x240 is a good balance
- Use `sixel_vfr` encoder to skip duplicate frames

### Frame Rate
- Enable `-F 1` (dropframe) for adaptive frame dropping
- Use `-I 1` (ignoredelay) for maximum streaming speed
- Scene change detection automatically adjusts to content

### Quality vs Speed
| Setting | Speed | Quality | Bandwidth |
|---------|-------|---------|-----------|
| `-c 16 -D none` | ⚡⚡⚡ | ⭐ | 💾 |
| `-c 32 -D none` | ⚡⚡ | ⭐⭐ | 💾💾 |
| `-c 32 -D fs` | ⚡ | ⭐⭐⭐ | 💾💾 |
| `-c 256 -D fs` | ⚡ | ⭐⭐⭐⭐ | 💾💾💾 |

### Dithering Algorithms (`-D`)
- **none** - Fastest, no dithering
- **atkinson** - Good balance
- **fs** (Floyd-Steinberg) - High quality, recommended
- **jajuni** - Jarvis-Judice-Ninke algorithm
- **stucki** - Similar to fs
- **burkes** - Another FS variant

## Project Structure

```
.
├── ffmpeg/              # FFmpeg submodule with SIXEL support
│   ├── libavdevice/     # SIXEL output device implementation
│   │   └── sixel.c      # Main SIXEL encoder with both muxers
│   └── ...
├── telfwd/              # Go TCP broadcast server
│   ├── main.go          # Server implementation
│   ├── sendkey.sh       # xdotool input handler
│   ├── go.mod           # Go dependencies
│   └── go.sum
├── start.bash           # Service orchestration script
└── README.md            # This file
```

## Development

### Modifying FFmpeg SIXEL encoder
The SIXEL encoder is in `ffmpeg/libavdevice/sixel.c`. After modifications:
```bash
cd ffmpeg
make -j$(nproc)
```

## See Also

- [FFmpeg SIXEL encoder](https://github.com/vasilevp/FFmpeg-SIXEL)
- [libsixel](https://github.com/saitoha/libsixel)
- [SIXEL Graphics](https://en.wikipedia.org/wiki/Sixel)
