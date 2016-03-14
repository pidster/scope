--[[
Copyright (C) 2015 Luca Marturana

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License version 2 as
published by the Free Software Foundation.


This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
--]]

-- Common function used by http parsing chisels

partial_transactions = {}

function http_init()
    chisel.set_filter("evt.is_io = true and evt.buflen > 0 and (fd.sockfamily = ip or fd.sockfamily = unix)")
    buffer_field = chisel.request_field("evt.buffer")
    fd_field = chisel.request_field("fd.num")
    pid_field = chisel.request_field("proc.pid")
    rawtime_field = chisel.request_field("evt.rawtime")
    datetime_field = chisel.request_field("evt.datetime")
    dir_field = chisel.request_field("evt.io_dir")

    container_field = chisel.request_field("container.name")

    sysdig.set_snaplen(1024)
end

function parse_request(req_buffer)
    method, url = string.match(req_buffer, "^(%u+) (%g+)")
    if method and url then
        host = string.match(req_buffer, "Host: (%g+)%.%.")
        return {
            method=method,
            url=url,
            host=host
        }
    end

    return nil
end

function parse_response(resp_buffer)
    resp_code = string.match(resp_buffer, "HTTP/[%g]+ (%d+)")
    if resp_code then
        content_length = string.match(resp_buffer, "Content%-Length: (%d+)%.%.")
        if not content_length then
            content_length = 0
        end
        return {
          code = tonumber(resp_code),
          length = tonumber(content_length)
        }
    else
        return nil
    end
end

function run_http_parser(evt, on_transaction)
    buf = evt.field(buffer_field)
    fd = evt.field(fd_field)
    pid = evt.field(pid_field)
    evt_dir = evt.field(dir_field)
    key = string.format("%d\001\001%d", pid, fd)

    timestamp = evt.field(rawtime_field)

    transaction = partial_transactions[key]
    if not transaction then
        request = parse_request(buf)
        if request then
            transaction_dir = "<NA>"
            if evt_dir == "read" then
                transaction_dir = "<"
            elseif evt_dir == "write" then
                transaction_dir = ">"
            end
            request["ts"] = timestamp
            partial_transactions[key] = {
                pid = pid,
                request= request,
                dir=transaction_dir,
                container=evt.field(container_field)
            }
        end
    else
        response = parse_response(buf)
        if response then
            transaction["response"] = response
            transaction["response"]["ts"] = timestamp
            on_transaction(transaction)
            partial_transactions[key] = nil
        end
    end
end

-- Chisel description
description = "Show a log of all HTTP requests";
short_description = "HTTP requests log";
category = "Application";
args = {}

-- Initialization callback
function on_init()
    http_init()
    -- The -pc or -pcontainer options was supplied on the cmd line
    print_container = sysdig.is_print_container_data()

    return true
end

function on_transaction(transaction)
    if print_container then
        container = string.format(" container=%s ", transaction["container"])
    else
        container = " "
    end
    print(string.format("rawtime=%d%sdirection=%s pid=%d method=%s url=%s host=%s response_code=%d latency=%dms size=%d",
                evt.field(rawtime_field),
                container,
                transaction["dir"],
                transaction["pid"],
                transaction["request"]["method"],
                transaction["request"]["url"],
                transaction["request"]["host"],
                transaction["response"]["code"],
                (transaction["response"]["ts"] - transaction["request"]["ts"])/1000000,
                transaction["response"]["length"]
            ))
end

function on_event()
    run_http_parser(evt, on_transaction)
end
