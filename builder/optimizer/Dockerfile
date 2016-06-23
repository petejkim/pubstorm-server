FROM node:6.2
ADD package.json /tmp/optimizer/package.json

WORKDIR /tmp/optimizer
RUN npm install -g gulp
RUN npm install

ADD gulpfile.js /tmp/optimizer/gulpfile.js

CMD ["gulp"]
