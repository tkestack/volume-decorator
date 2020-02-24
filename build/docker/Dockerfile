FROM ceph/ceph:v14.2
LABEL description="TKE Volume Decorator"

COPY volume-decorator /volume-decorator
RUN chmod +x /volume-decorator
ENTRYPOINT ["/volume-decorator"]
