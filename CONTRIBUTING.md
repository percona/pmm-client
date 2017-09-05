# Contributing notes

## Local setup

The easiest way to make a local development setup is to use Docker Compose.

```
docker-compose up
make
```


## Vendoring

We use [Glide](https://glide.sh) to vendor dependencies. Please use released version of Glide (i.e. do not `go get`
from `master` branch). Also please use `--strip-vendor` flag.
