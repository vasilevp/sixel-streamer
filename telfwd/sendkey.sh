#!/usr/bin/env bash
# Read commands from stdin and run them with xdotool
while IFS= read -r line; do
  # skip empty lines
  [ -z "$line" ] && continue
  # run the command
  xdotool $line
done