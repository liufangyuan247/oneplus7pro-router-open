"""Sequential orchestration via SearXNG's Python/offline engine interface."""
from router_search import get_router

engine_type = 'offline'
categories = ['general']
paging = False
language_support = False
time_range_support = False
timeout = 23.0


def search(query, params):
    return get_router().search(query, 'auto')
