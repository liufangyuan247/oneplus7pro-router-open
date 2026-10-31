"""Single-process WSGI launcher; the upstream SearXNG source is unchanged."""
import os

from waitress import serve
from searx.webapp import app

if __name__ == '__main__':
    serve(
        app,
        host=os.environ.get('SEARXNG_BIND_ADDRESS', '127.0.0.1'),
        port=int(os.environ.get('SEARXNG_PORT', '8888')),
        threads=2,
        connection_limit=16,
        backlog=16,
        channel_timeout=30,
        max_request_body_size=65536,
        clear_untrusted_proxy_headers=True,
    )
