Rise Server
===========

## Dependencies

### PostgreSQL

```shell
# Install PostgreSQL 9.4
sudo bash -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -
sudo apt-get update
sudo apt-get install postgresql-9.4 libpq-dev postgresql-contrib-9.4
sudo su postgres -c 'createuser --superuser nitrous && createdb rise_development'
```

### RabbitMQ

```shell
# Install RabbitMQ 3.6.2
echo 'deb http://www.rabbitmq.com/debian/ testing main' | sudo tee -a /etc/apt/sources.list
wget https://www.rabbitmq.com/rabbitmq-signing-key-public.asc
sudo apt-key add rabbitmq-signing-key-public.asc
sudo apt-get update
sudo apt-get install rabbitmq-server=3.6.2-1

# Add admin user
sudo rabbitmqctl add_user admin password
sudo rabbitmqctl set_user_tags admin administrator
sudo rabbitmqctl set_permissions -p / admin ".*" ".*" ".*"
sudo rabbitmqctl add_vhost rise_development
sudo rabbitmqctl add_vhost rise_test
sudo rabbitmqctl set_permissions -p rise_development admin ".*" ".*" ".*"
sudo rabbitmqctl set_permissions -p rise_test admin ".*" ".*" ".*"

# Enable management plugin
sudo rabbitmq-plugins enable rabbitmq_management

# You can open rabbitmq management page via 15672
```

### Redis

```shell
# Install Redis 3.0
# If you see `add-apt-repository: command not found`, please run `sudo apt-get install software-properties-common` first
sudo add-apt-repository ppa:chris-lea/redis-server
sudo apt-get update
sudo apt-get install redis-server
```

### Docker

```shell
# Install Docker 1.10.0 in Nitrous Ubuntu box
sudo apt-get install apt-transport-https ca-certificates
sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
echo 'deb https://apt.dockerproject.org/repo ubuntu-trusty main' | sudo tee -a /etc/apt/sources.list.d/docker.list
sudo apt-get update
sudo apt-get purge lxc-docker
apt-cache policy docker-engine
sudo apt-get update
sudo apt-get install linux-image-extra-$(uname -r)
sudo apt-get install apparmor
sudo apt-get install docker-engine=1.10.0-0~trusty
sudo usermod -aG docker nitrous
```
Log out from your box after complete the steps above.

1. Go to https://www.nitrous.io/legacy/#/containers/{CONTAINER_SLUG}/config.
2. Update Volume Mounts to Volume(...:/var/lib/docker) Mount Point(/var/lib/docker)
3. Enable "Privileged Mode"
4. Log in

## Vendoring

```shell
# Install govendor
go get -u github.com/kardianos/govendor

# Vendor additional dependencies
script/savedeps
```

## DB Migrations

```shell
# Install migrate
go get -u github.com/mattes/migrate

# Run migrations
script/migrate up

# WARNING: Doing script/migrate down will undo all migrations!
# Use script/migrate migrate instead!

# Creating a new migration
script/migrate-new 'create_animals'

# Please try doing script/migrate redo to make sure both up/down work
```

## Test

```shell
# Install Ginkgo/Gomega
go get -u github.com/onsi/ginkgo/ginkgo
go get -u github.com/onsi/gomega

# Prepare test DB
script/migrate up # Make sure dev DB is up to date
script/prepare-test-db # Copy dev DB's schema to test DB

# Run tests
script/test
```

## Run Server
```shell
# Create .env file from .env-example and edit AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY
cp .env-example .env

# Install forego
go get -u github.com/ddollar/forego

# Run server and workers
forego start
```

## Update OAuth client for rise-cli

The [rise-cli](https://github.com/nitrous-io/rise-cli-go) is an OAuth client of rise-server. The dev database is seeded with a record in the `oauth_clients` table but with random values for the client ID and secret. We have to set [proper values](https://github.com/nitrous-io/rise-cli-go/blob/master/script/build) so that it can actually make API requests to your development rise-server.

```shell
psql rise_development -c " \
UPDATE oauth_clients SET client_id='73c24fbc2eb24bbf1d3fc3749fc8ac35', client_secret='0f3295e1b531191c0ce8ccf331421644d4c4fbab9eb179778e5172977bf0238cdbf4b3afe1ead11b9892ce8806e87cc1acc10263dfdade879a05b931809690a1' WHERE id = 1;
"
```


- - -
Copyright (c) 2016 Nitrous, Inc. All Rights Reserved.
