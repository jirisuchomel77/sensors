## Sensors: The Log Analyzer

This application analyzes the logs coming from the production of the sensor manufacturer, as per the assignment:

"Your task is to process the log files and automate the quality control evaluation. The evaluation criteria are as follows:
1) For a thermometer, it is branded “ultra precise” if the mean of the readings is within 0.5 degrees of the known temperature, and the standard
deviation is less than 3. It is branded “very precise” if the mean is within 0.5 degrees of the room, and the standard deviation is under 5. Otherwise,
it’s sold as “precise”.
2) For a humidity sensor, it must be discarded unless it is within 1 humidity percent of the reference value for all readings. (All humidity sensor
readings are a decimal value representing percent moisture saturation.)"

## Requirements

* Kubernetes cluster with default storage class.
* kubectl
* REDIS server (can be external one, or deployed as part of the _sensors_ application)

User needs to know URL to the HTTP server which contains the directory with log files. Described in more details bellow.

## Installation

The application is deployed into the Kubernetes cluster using the manifest file `sensors-deployment.yaml`.

Before actual deployment, file must be updated with the correct values. Let's take a look at this snippet of the manifest file:

```
        env:
          - name: REDIS_HOST
            value: "redis"
          - name: REDIS_PORT
            value: "6379"
          - name: REMOTE_LOGS_DIR
            value: "http://apache/files/"
```

Here you can see the environment values that need to be set for the application containers.

`REDIS_HOST` and `REDIS_PORT` are pointing to the REDIS instance. You can leave the default values if you deploy redis using `redis-deployment.yaml` manifest file.

`REMOTE_LOGS_DIR` points to the URL with the log files. The assumption is that this points to the directory (exposed with Apache directory listing), and that the files are sorted from the newest to the oldes ones.

You can also update the `image` value with custom built image of `sensors` application, of course.

Once the manifest is sufficiently modified, proceed with

```bash
kubectl apply -f sensors-deployment.yaml
kubectl apply -f redis-deployment.yaml # optional
```

## Building from source

Use provided Makefile to run unit tests with 

```bash
make test
```

build the application with

```bash
make
```

or build and publish the docker image:

```bash
export IMG=${your-registry/image-name:tage}
make docker-build
make docker-push
```

Newly created image can be used in the manifest file when deploying into the Kubernetes cluster.

## Discussing the solution

The proposed algoritm looks like this:

1. Find the log file
2. Process the log file:
  * Read the reference values
  * Process each line
  * Once we read all necessary information about a sensor, decide its branding
3. Print/show the results.

Part 2 is just implementing the rules that are described in the assignemnt. The tricky parts are 1 and 3.

### Finding the log file

Where are the log files we need to process? What can we assume about the application that is generating them?

There are several options:

* Log files are stored "localy", meaning that the application running in kubernetes pods can access them from shared Persistent Volume.
  But this means log generating application needs to be installed in the same k8s cluster.

* It is much more likely that we do not have control over the log generating application. It is just running somewhere and producing log files.
  It is possible to assume that the log files are available in some remote directory, served by HTTP server. The "log generator" does not necessary
  need to contain HTTP server itself; there could actually be different application; one that fetches logs from log generator and serves them the
  way we need.
  For simplicity, let's assume the HTTP server supports directory listing, the log files are in the directory ordered from the newest to the oldest one
  and that there's some easily recognizamle naming pattern we can use to distinguish links to the log files from any other link on the page.

* As a combination of above options, there's a third one: we could have extra process (Pod) in our k8s cluster, taking care of fetching the logs
  from remote place (e.g. by periodic running rsync) and saving them into the Persistent Volume. Log files could be read from this PV by `sensors`
  application.
  The problem here is that we'd need a storageClass that makes PVC accessible from all cluster _nodes_ which is not the case for just any k8s cluster.

* GitOps: log files are pushed into git repository. Whenever the repository is updated, event triggers the pipeline (e.g. using Tekton) that runs the
  log analyzer (e.g. as a job; or it could just pass an argument poiting to the new log file to the running pod).


I chose second option. The algoritm to look for the newest log file then goes like this:

- read the page with directory listing
- process all the http links on the page from top
- when a log file is found, check if it was already processed. If it already was, go back and repeat (because of the assumption about sorting, all
  other log files are already processed too).

Now, how to find out if the file was already processed? Obviously we need to save some state. 
I chose to use external REDIS cache to save the information about the processed log files. We're using file names as the (unique) keys and whenever
REDIS already contains given key, it means simple indication that the file was already processed.

Alternatively, we could use Persistent volumes similar kind of information. However, as there could be many pods running the application, we'd need
to have PVC shared between them and implement some kind of locking whenever pod needs to write the state.

REDIS seems just simpler solution here: all pods are accessing same REDIS instance. Ideally, we'd use some locking too (there are libraries offering
this) that would prevent the situation where multiplie pods are processing the same log file.

### The output

According to the assignment, it seems that the application should just produce json-like structure describing the state of the sensors in given
log file.

As a simplest solution, we could just print such result to the standard output. This works, but is hardly usable approach for anyone who wants to
find out the results.

So, again, the best way seems some external service. As I already had REDIS instance as a cache for indicating which file was processed, I decided
to use it to store the final results too.
This is probably not the best option: real database, providing SQL like queries would be the right choice.

Another question is _what_ to save? As the assignment expects those json's as outputs per log file, this is the way it is implemented. REDIS keys
are file names and values are json structures describing the log files.

However it would be more logical to actually save the state of _sensors_ directly, not the log files. Who cares about the log files?
It would be rather easy to implement this: assuming sensor names are unique we could use same redis instance (possibly with different database?) and save 
just key value pairs of `"sensor-name": "branding"`


### Missing/incomplete

- no health checks are configured for sensors application
- test cases only cover the main algorithm, not any kind of integration (we could use docker-compose or simple k8s cluster do more)

## Summarizing assumptions:

  - target directory can be accessed as http page, listing log files from newest to oldest.
    (apparently there's a way to configure apache to do this)
  - whenever new log file appears in target directory, it is considered complete and no one will continue writing to it

