FROM ubuntu:14.04
MAINTAINER Nitrous.IO <eng@nitrous.io>

# Install essentials
RUN apt-get update && apt-get install -y --no-install-recommends \
  apparmor \
  build-essential \
  ca-certificates \
  curl \
  git-core \
  mercurial \
  libpq-dev \
  nodejs \
  python-pip \
  wget

# Install s3cmd
RUN cd /tmp && wget https://github.com/s3tools/s3cmd/releases/download/v1.6.1/s3cmd-1.6.1.tar.gz && \
  tar xzvf s3cmd-1.6.1.tar.gz && cd /tmp/s3cmd-1.6.1 && \
  python setup.py install && rm -rf /tmp/s3cmd-1.6.1*

# Install postgres 9.4
RUN echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" | tee /etc/apt/sources.list.d/postgresql.list && \
wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - && \
apt-get update && \
apt-get install -y postgresql-9.4 libpq-dev postgresql-contrib-9.4

# Create the databases
RUN service postgresql start && \
  sudo su - postgres -c 'createuser rise_test && createuser rise_development && createdb -O rise_development rise_development && createdb -O rise_test rise_test' && \
  sudo su - postgres -c 'createuser --superuser root' && \
  service postgresql stop

# Install go 1.6
RUN \
  mkdir -p /usr/local/opt && \
  mkdir -p /tmp/go-1.6 && \
  curl -s https://storage.googleapis.com/golang/go1.6.linux-amd64.tar.gz | tar -xvz -C /tmp/go-1.6 && \
  mv /tmp/go-1.6/go /usr/local/go-1.6 && \
  cd /usr/local && \
  ln -s go-1.6 go

# Install rabbitmq
RUN echo 'deb http://www.rabbitmq.com/debian/ testing main' | tee /etc/apt/sources.list.d/rabbitmq.list && \
  wget https://www.rabbitmq.com/rabbitmq-signing-key-public.asc && \
  apt-key add rabbitmq-signing-key-public.asc && \
  apt-get update && \
  apt-get install -y rabbitmq-server=3.6.1-1

# Create the proper rabbitmq configuration
# TODO: Investigate how to persist rabbitmq changes on docker-build
# RUN /etc/init.d/rabbitmq-server start && rabbitmqctl add_user admin password && \
#   rabbitmqctl set_user_tags admin administrator && \
#   rabbitmqctl set_permissions -p / admin ".*" ".*" ".*" && \
#   rabbitmqctl add_vhost rise_development && \
#   rabbitmqctl add_vhost rise_test && \
#   rabbitmqctl set_permissions -p rise_development admin ".*" ".*" ".*" && \
#   rabbitmqctl set_permissions -p rise_test admin ".*" ".*" ".*" && \
#   /etc/init.d/rabbitmq-server stop

ENV GOPATH /opt/
ENV GOBIN /opt/bin
ENV PATH /usr/local/go/bin:/opt/bin:$PATH
ENV GOROOT /usr/local/go

# RUN go get -u github.com/kardianos/govendor
# RUN go get -u github.com/onsi/ginkgo/ginkgo
# RUN go get -u github.com/onsi/gomega
# RUN go get -u github.com/mattes/migrate
