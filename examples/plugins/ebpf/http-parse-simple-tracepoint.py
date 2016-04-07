from bcc import BPF
from time import sleep, strftime

b = BPF(src_file="http-parse-simple-tracepoint.c")
print BPF.open_kprobes()
while True:
    try:
        sleep(2)
    except KeyboardInterrupt:
        exit()
    print "[%s]" % strftime("%H:%M:%S")
    data = b.get_table("received_http_requests")
    data = sorted(data.items(), key=lambda kv: kv[1].value)
    for key, value in data:
        print "\t%-10s %s" % (key.pid ,str(value.value))
