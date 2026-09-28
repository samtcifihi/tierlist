# This project uses just, not make.
# Go recipes will be added along with the Go code.

set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

default:
    @just --list
