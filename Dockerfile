FROM golang:1

ENV PROJECT=splunk-event-reader
ENV ORG_PATH="github.com/Financial-Times"
ENV REPO_PATH="${ORG_PATH}/${PROJECT}"
ENV DATETIME="dateTime=$(date -u +%Y%m%d%H%M%S)"
ENV SRC_FOLDER="${GOPATH}/src/${ORG_PATH}/${PROJECT}"

COPY . ${SRC_FOLDER}
WORKDIR ${SRC_FOLDER}

RUN BUILDINFO_PACKAGE="${ORG_PATH}/${PROJECT}/vendor/${ORG_PATH}/service-status-go/buildinfo." \
  && VERSION="version=$(git describe --tag --always 2> /dev/null)" \
  && DATETIME="dateTime=$(date -u +%Y%m%d%H%M%S)" \
  && REPOSITORY="repository=$(git config --get remote.origin.url)" \
  && REVISION="revision=$(git rev-parse HEAD)" \
  && BUILDER="builder=$(go version)" \
  && LDFLAGS="-X '"${BUILDINFO_PACKAGE}$VERSION"' -X '"${BUILDINFO_PACKAGE}$DATETIME"' -X '"${BUILDINFO_PACKAGE}$REPOSITORY"' -X '"${BUILDINFO_PACKAGE}$REVISION"' -X '"${BUILDINFO_PACKAGE}$BUILDER"'" \


# Multi-stage build - copy only the certs and the binary into the image
FROM scratch
WORKDIR /
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=0 /artifacts/* /

CMD [ "/splunk-event-reader" ]