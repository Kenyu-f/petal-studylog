# Multi-stage build: compile the (dependency-free) Go binary in a full Go
# image, then ship it in a minimal runtime image. See explanation.md,
# "Deploying StudyLog", for how this is used with Railway/Fly.io.

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
# CGO_ENABLED=0: static binary, no libc dependency needed in the
# minimal runtime image below.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/studylog .

FROM alpine:3.20
RUN adduser -D -u 10001 studylog
WORKDIR /app
COPY --from=build /out/studylog ./studylog
COPY templates ./templates
COPY static ./static
# The data directory is where the JSON store writes by default; a
# platform volume should be mounted here (or DATA_PATH pointed at the
# volume's mount path instead — see explanation.md).
RUN mkdir -p /data && chown -R studylog:studylog /app /data
USER studylog

ENV DATA_PATH=/data/studylog.json
EXPOSE 8080

ENTRYPOINT ["./studylog"]
