.PHONY: run clean

EXE=sysdig-plugin
UPTODATE=.$(EXE).uptodate

run: $(UPTODATE)
	docker run --rm -it --privileged -v /var/run/scope/plugins:/var/run/scope/plugins -v /var/run/docker.sock:/host/var/run/docker.sock -v /dev:/host/dev -v /proc:/host/proc:ro -v /boot:/host/boot:ro -v /lib/modules:/host/lib/modules:ro -v /usr:/host/usr:ro --name sysdig-plugin sysdig-plugin /usr/bin/sysdig-plugin -hostname=$(shell hostname)

$(UPTODATE): $(EXE) Dockerfile chisels/*
	docker build -t sysdig-plugin .
	touch $@

$(EXE): main.go
	docker run --rm -v "$$PWD":/usr/src/$(EXE) -w /usr/src/$(EXE) golang:1.6 go build -v

clean:
	- rm -rf $(UPTODATE) $(EXE)

