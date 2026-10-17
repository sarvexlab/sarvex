FROM golang:1.26 as build

WORKDIR /go/src/app
COPY . .

EXPOSE 8080

RUN go mod download
# RUN go vet ./...
# RUN go test -v ./...

RUN CGO_ENABLED=0 go build -o /go/bin/app

# Alpine rather than a shell-less image: the Helm charts start the binary
# through /bin/sh so they can source the Vault Agent Injector's rendered env
# file first (deployments/selfhost/helm/*/templates/deployment.yaml).
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /go/bin/app /
COPY --from=build /go/src/app/configs /configs

ENTRYPOINT [ "/app" ]
CMD ["--service", "", "--configPath", "", "--env", "", "--tls", "false", "--port", "8080"]
