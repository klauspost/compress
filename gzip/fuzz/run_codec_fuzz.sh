#!/usr/bin/env bash

go-fuzz -bin=codec-fuzz.zip -workdir=codec/workdir
