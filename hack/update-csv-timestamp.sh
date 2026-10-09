#!/bin/bash

if git diff --quiet -I'^( )+createdAt: ' bundle; then
    git checkout --quiet bundle
fi
