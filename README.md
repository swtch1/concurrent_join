# Concurrent Join

This little bit of code came from an interview excercise.  The purpose was to
create an HTTP server that could accept connections by calling the `/join`
endpoint.  The call will block and timeout if 10 seconds has passed without a
second joiner.  If a second caller joins both callers will return with the
order in which they called the endpoint.

The `/stats` endpoint will provide statistics for the pairs of connections, up
to the first 50k pairs.

