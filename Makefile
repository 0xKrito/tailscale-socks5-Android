GOOS ?= $(shell go env GOOS)

ts-proxy: *.go
ifeq ($(GOOS),android)
	go build -ldflags "-checklinkname=0 -s -w" -trimpath .
else
	go build -ldflags "-s -w" -trimpath .
endif
