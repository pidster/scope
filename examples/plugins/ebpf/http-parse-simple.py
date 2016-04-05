#!/usr/bin/python
#
#Bertrone Matteo - Polytechnic of Turin
#November 2015
#
#eBPF application that parses HTTP packets 
#and extracts (and prints on screen) the URL contained in the GET/POST request.
#
#eBPF program http_filter is used as SOCKET_FILTER attached to eth0 interface.
#only packet of type ip and tcp containing HTTP GET/POST are returned to userspace, others dropped
#
#python script uses bcc BPF Compiler Collection by iovisor (https://github.com/iovisor/bcc)
#and prints on stdout the first line of the HTTP GET/POST request containing the url

from __future__ import print_function
from bcc import BPF

import os
import socket
import struct
import sys
from time import sleep, strftime

if len(sys.argv) != 2:
  print("usage: %s <iface>" % sys.argv[0])
  sys.exit(1)

iface = sys.argv[1]

# initialize BPF - load source code from http-parse-simple.c
bpf = BPF(src_file = "http-parse-simple.c",debug = 0)

#load eBPF program http_filter of type SOCKET_FILTER into the kernel eBPF vm
#more info about eBPF program types
#http://man7.org/linux/man-pages/man2/bpf.2.html
function_http_filter = bpf.load_func("http_filter", BPF.SOCKET_FILTER)

#create raw socket, bind it to the interface
#attach bpf program to socket created
# TODO: attach this to all interfaces
BPF.attach_raw_socket(function_http_filter, iface)

#get file descriptor of the socket previously created inside BPF.attach_raw_socket
socket_fd = function_http_filter.sock

#create python socket object, from the file descriptor
sock = socket.fromfd(socket_fd,socket.PF_PACKET,socket.SOCK_RAW,socket.IPPROTO_IP)
#set it as blocking socket
sock.setblocking(True)

while True:
  try:
    sleep(2)
  except KeyboardInterrupt:
    exit()
  print ("[%s]" % strftime("%H:%M:%S"))
  data = bpf.get_table("received_http_requests")
  data = sorted(data.items(), key=lambda kv: kv[1].value)
  for key, value in data:
    print ("\t%-10s %s" % (key.pid ,str(value.value)))
