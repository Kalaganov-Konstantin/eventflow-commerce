FROM fluent/fluentd:v1.16-1

USER root

RUN apk add --no-cache --update build-base \
    # Pin the elasticsearch client to the 8.x line first: fluent-plugin-elasticsearch pulls in
    # whatever satisfies its dependency, which otherwise resolves to a 9.x client that sends a
    # "compatible-with=9" media type header our 8.x elasticsearch server rejects with a 400.
    && gem install elasticsearch -v "~> 8.13" --no-document \
    && gem install fluent-plugin-elasticsearch --no-document \
    && gem sources --clear-all \
    && apk del build-base \
    && rm -rf /tmp/* /var/tmp/* /usr/lib/ruby/gems/*/cache/*.gem

COPY fluent.conf /fluentd/etc/fluent.conf

# Stays root: /var/lib/docker/containers is only readable by root on the host, and this
# container's entire job is reading other containers' log files from there.
