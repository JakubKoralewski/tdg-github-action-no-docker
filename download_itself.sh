#!/bin/bash
set -e

# Arguments
ACTION_REPOSITORY=$1
FILE=$3

# Download the asset
curl -L -H "Authorization: token $TOKEN" \
  -H "Accept: application/octet-stream" \
  "https://api.github.com/$ACTION_REPOSITORY/releases/latest/download/$FILE" \
  -o "$FILE"
