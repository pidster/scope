#include <uapi/linux/ptrace.h>
#include <net/sock.h>
#include <bcc/proto.h>

#define IP_TCP 	6   
#define ETH_HLEN 14

/*eBPF program.
  Filter IP and TCP packets, having payload not empty
  and containing "HTTP", "GET", "POST" ... as first bytes of payload
  if the program is loaded as PROG_TYPE_SOCKET_FILTER
  and attached to a socket
  return  0 -> DROP the packet
  return -1 -> KEEP the packet and return it to user space (userspace can read it from the socket_fd )
*/

struct received_http_requests_key_t {
  u32 pid;
};
BPF_HASH(received_http_requests, struct received_http_requests_key_t, u64);


int http_filter(struct __sk_buff *skb) {
  const int DROP = 0;
  const int KEEP = -1;

	u8 *cursor = 0;

	struct ethernet_t *ethernet = cursor_advance(cursor, sizeof(*ethernet));
	//filter IP packets (ethernet type = 0x0800)
	if (!(ethernet->type == 0x0800)) {
		return DROP;	
	}

	struct ip_t *ip = cursor_advance(cursor, sizeof(*ip));
	//filter TCP packets (ip next protocol = 0x06)
	if (ip->nextp != IP_TCP) {
		return DROP;
	}

	u32  tcp_header_length = 0;
	u32  ip_header_length = 0;
	u32  payload_offset = 0;
	u32  payload_length = 0;

	struct tcp_t *tcp = cursor_advance(cursor, sizeof(*tcp));

	//calculate ip header length
	//value to multiply * 4
	//e.g. ip->hlen = 5 ; IP Header Length = 5 x 4 byte = 20 byte
	ip_header_length = ip->hlen << 2;    //SHL 2 -> *4 multiply
		
	//calculate tcp header length
	//value to multiply *4
	//e.g. tcp->offset = 5 ; TCP Header Length = 5 x 4 byte = 20 byte
	tcp_header_length = tcp->offset << 2; //SHL 2 -> *4 multiply

	//calculate patload offset and length
	payload_offset = ETH_HLEN + ip_header_length + tcp_header_length; 
	payload_length = ip->tlen - ip_header_length - tcp_header_length;
		  
	//http://stackoverflow.com/questions/25047905/http-request-minimum-size-in-bytes
	//minimum length of http request is always geater than 7 bytes
	//avoid invalid access memory
	//include empty payload
	if(payload_length < 7) {
		return DROP;
	}

	//load firt 7 byte of payload into p (payload_array)
	//direct access to skb not allowed
	unsigned long p[7];
	int i = 0;
	int j = 0;
	for (i = payload_offset ; i < (payload_offset + 7) ; i++) {
		p[j] = load_byte(skb , i);
		j++;
	}

	//find a match with an HTTP message
	//HTTP
	if ((p[0] == 'H') && (p[1] == 'T') && (p[2] == 'T') && (p[3] == 'P')) {
          
          /* Record request */
          struct received_http_requests_key_t __key = {};
          __key.pid = bpf_get_current_pid_tgid() & 0xFFFF;
         received_http_requests.increment(__key);
	}
  /* TODO:
   * Note: This probably won't handle http pipelining, and http2
   */

	//no HTTP match
  return DROP;
}
