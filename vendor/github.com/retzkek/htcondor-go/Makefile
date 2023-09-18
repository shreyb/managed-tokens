.PHONY: test
test: | docker wait-for-condor submit-in-docker wait-for-jobs test-in-docker

.PHONY: docker
docker:
	-docker rm -f htcondor-go-test
	docker build -t htcondor-go-test .
	docker run -d --name htcondor-go-test htcondor-go-test

.PHONY: wait-for-condor
wait-for-condor:
	sleep 10

.PHONY: submit-in-docker
submit-in-docker:
	docker exec htcondor-go-test su -l tester -c 'condor_submit hello.sub && condor_submit hello_neverrun.sub'

.PHONY: test-in-docker
test-in-docker:
	docker exec htcondor-go-test go test

.PHONY: wait-for-jobs
wait-for-jobs:
	sleep 60

.PHONY: clean
clean:
	-docker rm -f htcondor-go-test
