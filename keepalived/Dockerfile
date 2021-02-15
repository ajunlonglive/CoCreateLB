FROM ubuntu:18.04

ARG KEEPALIVED_VERSION=2.0.6

RUN apt-get update \
 && apt-get install -y build-essential libssl-dev curl \
 && curl -o /tmp/keepalived.tar.gz -SL http://www.keepalived.org/software/keepalived-${KEEPALIVED_VERSION}.tar.gz \
 && cd /tmp \
 && tar -xzf keepalived.tar.gz \
 && cd /tmp/keepalived* \
 && ./configure \
 && make \
 && make install \
 && apt-get remove build-essential libssl-dev -y \
 && apt-get autoremove -y \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/* /var/log/apt

CMD ["/usr/bin/startup.sh"]
COPY /startup.sh /usr/bin/startup.sh
