FROM centos:7
LABEL description="TKE Volume Decorator"

ENV CEPH_VERSION "mimic"
RUN yum  install -y centos-release-ceph && \
    yum install -y kmod ceph-common ceph-fuse attr && \
    yum clean all && \
    unset http_proxy && unset https_proxy

COPY volume-decorator /volume-decorator
RUN chmod +x /volume-decorator
ENTRYPOINT ["/volume-decorator"]
