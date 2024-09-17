# --privileged \
# --security-opt label=disable \
# --security-opt apparmor=unconfined \
# --cap-add CAP_CHOWN \
# --cap-add=setuid \
# --cap-add=setgid \

run:
	podman run -ti --rm -d \
		--annotation run.oci.keep_original_groups=1 \
		--group-add keep-groups \
		--userns=keep-id \
		--ulimit nofile=262144:262144 \
		--cap-add=SYS_NICE \
		--cap-add=IPC_LOCK \
		--cap-add=NET_ADMIN \
		-v ./db/:/var/lib/clickhouse/:Z \
		-v ./initdb.d/:/docker-entrypoint-initdb.d/:Z \
		-v ./logs/:/var/log/clickhouse-server/:Z \
		clickhouse/clickhouse-server:24.3.5.46-alpine
# -v ./xtcppb.proto:/var/lib/clickhouse/format_schemas/xtcppb.proto:Z \
# -v ./initdb.d/:/docker-entrypoint-initdb.d/:Z \
# -v ./xtcppb.proto:/var/lib/clickhouse/format_schemas/xtcppb.proto:Z \
#		--privileged \

up:
#podman-compose -f docker-compose.yml up
	podman-compose up

ch_server_up:
	pwd=${PWD} \
	podman compose \
		--file ./docker-compose.yml \
		up -d --remove-orphans --force-recreate

	@echo "podman exec -ti clickhouse-ch_server-1 bash"
	@echo "podman exec -it clickhouse-ch_server-1 clickhouse-client"
	@echo ------------

ch_server_down:
	PWD=${PWD} \
	podman compose \
		--file ./docker-compose.yml \
		down --remove-orphans

#	docker compose \
#		--env-file deployments/local/enviroment-variables \
#		--file deployments/local/docker-compose.updater.yml \
#		up -d --remove-orphans