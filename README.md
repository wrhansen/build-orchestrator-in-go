# Build an Orchestrator in Go

My go through of the book "Build an Orchestrator in Go"
You can find the book here: https://www.amazon.com/dp/1617299758/

There is a companion repo for the book which you can find here:
https://github.com/buildorchestratoringo/code


# Notes & Errata

## Chapter 9

Had to run `docker run -p 7777:7777 --name echo --rm --platform linux/amd64 timboring/echo-server:latest`
to get this running on macos. The `--platform` arg was necessary to stop the error
about the host platform not maching the platform of the image.


## Chapter 10

To run a worker and manager from the command line enter:

```sh
$ CUBE_WORKER_HOST=localhost CUBE_WORKER_PORT=5556 CUBE_MANAGER_HOST=localhost CUBE_MANAGER_PORT=5555 go run main.go
```

* We're not running worker.CollectStats() anymore?
