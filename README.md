# jisho-api

Tiny scraper based API app for [jisho.org](https://jisho.org). すげーじゃん？

## Configuration 

``jisho-api`` is configured through ENV vars:


```bash
JISHO_API_HOST         # (default: "") address jisho-api will listen on. an empty string means all available interfaces/addresses
JISHO_API_PORT         # (default: "8080") port jisho-api will listen on

JISHO_API_LOG_JSON     # (default: false) whether or not jisho-api should log using a json format
JISHO_API_LOG_CONCISE  # (default: false) whether or not jisho-api should use 'concise logging' (reduces request log output)
JISHO_API_LOG_LEVEL    # (default: "info") one of "trace", "debug", "info", "warn", "error", "critical"

JISHO_API_REDIS_ADDR   # (default: "") address of the redis server to use. jisho-api will crash when this is configured but it cant connect.
JISHO_API_REDIS_PASS   # (default: "") password to use when connecting with the redis server. leave blank if no password is required
JISHO_API_REDIS_DB     # (default: 0)  db to use. leave blank to use 0 / the default db
```

### Redis cache

``jisho-api`` can make use of redis to speed up its responses and most importantly stop hammering jisho.org with request it has already made.
To enable it simply point the ``JISHO_API_REDIS_ADDR`` env var to a valid redis server and ``jisho-api`` will do the rest. Note that if the redis
server cannot be accessed ``jisho-api`` will crash at launch.


## Searching things

Simply make a request to ``<host>/search/<search query>`` in the same way you would search jisho.org. Additional results (if there any) can be
requested by specifying the ``page`` query param (starts counting from 1).

```bash
$ curl -s my-jisho-api.com/search/たべる | jq
[
  {
    "word": "食べる",
    "reading": "食(た)べる",
    "meanings": [
      {
        "value": "to eat",
        "tags": [
          "Ichidan verb",
          "Transitive verb"
        ]
      },
      {
        "value": "to live on (e.g. a salary); to live off; to subsist on",
        "tags": [
          "Ichidan verb",
          "Transitive verb"
        ]
      }
    ],
    "tags": [
      "Common word",
      "JLPT N5",
      "Wanikani level 6"
    ]
  }
]

```