FROM emqx/nanomq:0.24-full

RUN apt-get update \
	&& apt-get install -y --no-install-recommends curl \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /nanomq/certs

COPY nanomq.conf.template /etc/nanomq.conf.template
COPY entrypoint.sh /usr/local/bin/lina-nanomq-entrypoint.sh
COPY healthcheck.sh /usr/local/bin/lina-nanomq-healthcheck.sh

RUN chmod +x /usr/local/bin/lina-nanomq-entrypoint.sh \
	/usr/local/bin/lina-nanomq-healthcheck.sh

# TLS listener is defined in nanomq.conf.template (certs mounted at /nanomq/certs). Keep env TLS off so
# it does not duplicate or override listeners.ssl (see Docker docs: NANOMQ_TLS_*_PATH vs wrong *_CERTFILE names).
ENV NANOMQ_TLS_ENABLE=false

ENTRYPOINT ["/usr/local/bin/lina-nanomq-entrypoint.sh"]
CMD ["start", "--conf", "/etc/nanomq.conf"]
