package clickhouse-inst

apiVersion: "clickhouse.altinity.com/v1"
kind:       "ClickHouseInstallation"
metadata: {
	name:      "clickhouse-inst"
	namespace: "clickhouse"
}
spec: {
	configuration: {
		zookeeper: {
			// [das@hp1:~]$ kubectl describe clickhousekeeperinstallation.clickhouse-keeper.altinity.com/extended -n clickhouse | grep -A 3 Fqdns
			//   Fqdns:
			//     chk-extended-cluster1-0-0.clickhouse.svc.cluster.local
			//     chk-extended-cluster1-0-1.clickhouse.svc.cluster.local
			//     chk-extended-cluster1-0-2.clickhouse.svc.cluster.local
			nodes: [{
				//port: 2181
				host: "chk-extended-cluster1-0-0.clickhouse.svc.cluster.local"
			}, {
				//port: 2181
				host: "chk-extended-cluster1-0-1.clickhouse.svc.cluster.local"
			}, {
				//port: 2181
				host: "chk-extended-cluster1-0-2.clickhouse.svc.cluster.local"
			}]
		}
		clusters: [{
			name: "clickhouse"
			layout: {
				shardsCount:   1
				replicasCount: 3
			}
		}]
	}
	defaults: templates: {
		podTemplate:             "pod-template-resource-limit"
		dataVolumeClaimTemplate: "data-volume-template"
		//serviceTemplate: svc-template
		logVolumeClaimTemplate: "log-volume-template"
	}
	templates: {
		podTemplates: [{
			name: "pod-template-resource-limit"
			spec: {
				containers: [{
					name: "clickhouse"
					// https://hub.docker.com/_/clickhouse
					image: "clickhouse/clickhouse-server:24.10"
					env: [{
						name:  "CLICKHOUSE_ALWAYS_RUN_INITDB_SCRIPTS"
						value: "true"
					}]
					resources: {
						requests: {
							memory: "8Gi"
							cpu:    1
						}
						limits: {
							memory: "12Gi"
							cpu:    4
						}
					}
					volumeMounts: [{
						name:      "data-volume-template"
						mountPath: "/var/lib/clickhouse"
					}, {
						name:      "log-volume-template"
						mountPath: "/var/log/clickhouse-server"
					}, {
						// - name: exampleprotobuf-configmap-volume
						//   mountPath: /var/lib/clickhouse/format_schemas/flatxtcppb.proto
						//   subPath: flatxtcppb.proto
						// mountPath: /var/lib/clickhouse/format_schemas/example.proto
						// subPath: example.proto
						name:      "flatprotobuf-configmap-volume"
						mountPath: "/var/lib/clickhouse/format_schemas/flatxtcppb.proto"
						subPath:   "flatxtcppb.proto"
					}, {
						name:      "bootstrap-configmap-volume"
						mountPath: "/docker-entrypoint-initdb.d"
					}]
				}]
				volumes: [{
					// - name: exampleprotobuf-configmap-volume
					//   configMap:
					//     name: exampleprotobuf-configmap
					name: "flatprotobuf-configmap-volume"
					configMap: name: "flatprotobuf-configmap"
				}, {
					name: "bootstrap-configmap-volume"
					configMap: {
						//defaultMode: 0755
						name: "bootstrap-mounted-configmap"
					}
				}]
			}
		}]
		volumeClaimTemplates: [{
			name: "data-volume-template"
			spec: {
				accessModes: [
					// https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes
					"ReadWriteOnce"]
				resources: requests: storage: "100Gi"
			}
		}, {
			name: "log-volume-template"
			spec: {
				accessModes: ["ReadWriteOnce"]
				resources: requests: storage: "1Gi"
			}
		}]
	}
}
// serviceTemplates:
//   - name: svc-template
//     generateName: chendpoint-{chi}
//     spec:
//       ports:
//         - name: http
//           port: 8123
//         - name: tcp
//           port: 9000
//       type: LoadBalancer
