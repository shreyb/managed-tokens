# Borrowed heavily from https://github.com/glideinWMS/containers/blob/main/worker/fnal-wn-sl7/Dockerfile
FROM scientificlinux/sl:7

RUN yum install -y wget sed

# Next setting up EPEL and OSG repositories
# OSG by default has a YUM  prio of 98
# Assigning EPEL YUM prio of 99
RUN yum install -y http://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm ;\
    yum install -y yum-priorities ;\
    yum install -y https://repo.opensciencegrid.org/osg/3.6/osg-3.6-el7-release-latest.rpm ;\
    /bin/sed -i '/^enabled=1/a priority=99' /etc/yum.repos.d/epel.repo ;\
    echo "priority=80" >> /etc/yum.repos.d/osg.repo

# install yum-conf-extras to enable sl-extras repo
RUN yum install -y yum-conf-extras

# Now install packages we need to run managed tokens
RUN yum install -y \
    osg-ca-certs \
    krb5-workstation \
    python36-cryptography \
    python3-six \
    jq \
    condor \
    condor-credmon-vault \
    iputils \
    rsync \
    sqlite


# Install Go
RUN /usr/bin/wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz ;\
    tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz

# Add Go to the PATH
ENV PATH="${PATH}:/usr/local/go/bin"

CMD ["/bin/bash"]
