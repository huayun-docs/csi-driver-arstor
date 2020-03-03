FROM alpine:3.10.3
LABEL maintainers="panfengyun"
LABEL description="ArStor CSI Driver"

# Add util-linux to get a new version of losetup.
RUN apk add util-linux multipath-tools e2fsprogs xfsprogs file xfsprogs-extra e2fsprogs-extra
COPY ./bin/arstorplugin /arstorplugin
ENTRYPOINT ["/arstorplugin"]

# debian
#FROM k8s.gcr.io/debian-base-amd64:1.0.0
#LABEL maintainers="panfengyun"
#LABEL description="ArStor CSI Driver"
#RUN clean-install ca-certificates e2fsprogs mount xfsprogs udev
#COPY ./bin/arstorplugin /arstorplugin
#ENTRYPOINT ["/arstorplugin"]
