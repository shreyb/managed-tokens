FROM golang:buster


# Install HTCondor
RUN wget -qO - http://research.cs.wisc.edu/htcondor/debian/HTCondor-Release.gpg.key | apt-key add - && \
    echo "deb http://research.cs.wisc.edu/htcondor/debian/8.8/buster/ buster contrib" >> /etc/apt/sources.list && \
    apt-get update && \
    apt-get -y install condor

# KNOBS and startup script
COPY htcondor-docker/condor_config.docker_image /etc/condor/config.d/
COPY htcondor-docker/start-condor.sh /usr/sbin/

# test user and job
RUN useradd -d /htcondor-test -m tester
COPY htcondor-docker/hello* /htcondor-test/

# Use this if you're not going to restart HTCondor in the container.
# If you do need to do that, you're better off running the condor_master
# command manually
CMD ["/usr/sbin/start-condor.sh"]

# Add app
WORKDIR /go/src/github.com/retzkek/htcondor-go
COPY . .
RUN go install
