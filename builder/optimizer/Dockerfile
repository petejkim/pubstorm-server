FROM node:latest

RUN npm install -g gulp && \
    mkdir /tmp/optimizer

ADD package.json /tmp/optimizer/package.json

WORKDIR /tmp/optimizer
RUN npm install

ADD gulpfile.js /tmp/optimizer/gulpfile.js

CMD ["gulp"]

