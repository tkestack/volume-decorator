FROM centos:7
LABEL description="TKE Volume Decorator"

ENV CEPH_VERSION "mimic"
RUN export http_proxy=http://proxy.tencent.com:8080 && https_proxy=http://proxy.tencent.com:8080 && \
    yum  install -y centos-release-ceph && \
    yum install -y kmod ceph-common ceph-fuse attr && \
    yum clean all && \
    unset http_proxy && unset https_proxy

COPY volume-manager /volume-manager
RUN chmod +x /volume-manager
ENTRYPOINT ["/volume-manager"]
