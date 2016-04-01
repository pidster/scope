FROM ubuntu
MAINTAINER Weaveworks Inc <help@weave.works>
LABEL works.weave.role=system
RUN sudo apt-key adv --keyserver keyserver.ubuntu.com --recv-keys D4284CDD
RUN echo "deb http://52.8.15.63/apt trusty main" | sudo tee /etc/apt/sources.list.d/iovisor.list
RUN sudo apt-get update && sudo apt-get install -y libbcc libbcc-examples python-bcc
ADD ./ebpf ./http-parse-simple.c ./http-parse-simple.py /usr/bin/
ENTRYPOINT ["/usr/bin/ebpf"]
