FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl python3 \
  && rm -rf /var/lib/apt/lists/*

COPY tools/deploy/stg-clouddeploy-task.sh /usr/local/bin/goatos-stg-clouddeploy-task

ENTRYPOINT ["/usr/local/bin/goatos-stg-clouddeploy-task"]
