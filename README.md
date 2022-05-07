# HTTP Handler module

This module implements `http.Handler` interface.
It accepts requests with the list of endpoints to fetch. The list of response sizes for each endpoint is returned. Duplicate endpoints are only fetched once. If the number of concurrent incoming requests exceeds 100 then `429 Too Many Requests` error is returned.

## Status codes

|Code|Body|Condition|
|--|--|--|
|200|List of response sizes for each of the requests endpoints (not sorted)|All requested endpoints have responded|
|207|List of response sizes for each of the requests endpoints (not sorted). If an endpoint did not respond, `-1` is written to the list|Some of the requested endpoints did not respond|
|405|—|Unsupported method. Only `POST` is supported
|408|—|None of the requested endpoints have responded|
|429|—|Concurrent request limit (100) is reached