#!/bin/bash

set -e

function exop() {
  echo exop "$@"
  go run ./cmd/exop "$@"
}

function logp() {
  echo logp "$@"
  go run ./cmd/logp "$@"
}

exop /describe-components

exop post /delete

exop /create-component \
  name=tick \
  type=process \
  'spec={"command": "./tick"}'

exop /create-component \
  name=echo \
  type=process \
  'spec={
    "command": "socat",
    "arguments": ["TCP4-LISTEN:2000,fork", "EXEC:cat"]
   }'

logp /