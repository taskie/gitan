#!/bin/bash
set -eu

cd cmd/gitan/
go build
./gitan "$@"
