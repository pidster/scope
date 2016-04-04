.PHONY: run clean

EXE=ebpf
IMAGE=weavescope-ebpf-plugin
UPTODATE=.$(EXE).uptodate

run: $(UPTODATE)
	docker run --rm -it \
	  --privileged --net=host \
	  -v /lib/modules:/lib/modules \
	  -v /usr/src:/usr/src \
	  -v /var/run/scope/plugins:/var/run/scope/plugins \
	  --name $(IMAGE) \
	  $(IMAGE) -hostname=$(shell hostname)

$(UPTODATE): $(EXE) Dockerfile
	docker build -t $(IMAGE) .
	touch $@

$(EXE): main.go
	docker run --rm -v "$$PWD":/usr/src/$(EXE) -w /usr/src/$(EXE) golang:1.6 go build -v -o $@ main.go

clean:
	- rm -rf $(UPTODATE) $(EXE)
	- docker rmi $(IMAGE)
