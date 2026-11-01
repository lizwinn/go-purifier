# SPDX-License-Identifier: MIT
#
# An HTTP proxy for tests/vcs-systems.sh that answers the go command's
# lookups of module paths (`?go-get=1`) with a go-import tag, so that a
# module path under example.invalid leads to a repository on this host.
#
#   python3 go-get-proxy.py <port> <module path> <vcs> <repository URL> ...
#
# It refuses CONNECT, so the go command falls back from HTTPS to HTTP for
# the module paths GOINSECURE names.
import http.server
import sys
import urllib.parse

port = int(sys.argv[1])
rest = sys.argv[2:]
imports = {rest[i]: (rest[i + 1], rest[i + 2]) for i in range(0, len(rest), 3)}


class Handler(http.server.BaseHTTPRequestHandler):
    def do_CONNECT(self):
        self.send_error(502, "no HTTPS through this proxy")

    def do_GET(self):
        url = urllib.parse.urlsplit(self.path)
        path = url.netloc + url.path
        for module, (vcs, repo) in imports.items():
            if path == module or path.startswith(module + "/"):
                body = (
                    '<html><head><meta name="go-import" content="%s %s %s"></head></html>'
                    % (module, vcs, repo)
                ).encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/html")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
        self.send_error(404)

    def log_message(self, format, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
