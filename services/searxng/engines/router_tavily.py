"""Explicit Tavily basic search, sharing the automatic engine's key pool."""
from router_search import get_router

engine_type = 'offline'
categories = ['general']
paging = False
language_support = False
time_range_support = False
timeout = 23.0
about = {'website': 'https://tavily.com', 'use_official_api': True, 'require_api_key': True}


def search(query, params):
    return get_router().search(query, 'tavily')
