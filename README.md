# xtcp2

Please run:
- "make" to build the docker containers and then start the set of containers
- "deploy" to start the containers if you've already built them

See also: "cat Makefile"


```
#define NDIAG_FLAG_LISTEN_ALL_NSID	0x00000010
```
https://github.com/torvalds/linux/blob/bcde95ce32b666478d6737219caa4f8005a8f201/include/uapi/linux/netlink_diag.h#L64

## How to do things in this repo

### Redpanda

Redpanda has a admin web console

http://localhost:8085/
http://localhost:8085/topics/xtcp?p=-1&s=50&o=-1#messages

### Clickhouse
To query clickhouse

```
docker exec -ti xtcp-clickhouse-1 bash
clickhouse-client
```

Or direct
```
[das@t:~/Downloads/xtcp2]$ docker exec -ti xtcp-clickhouse-1 clickhouse-client
ClickHouse client version 24.8.12.28 (official build).
Connecting to localhost:9000 as user default.
Connected to ClickHouse server version 24.8.12.

5d1ddc0e72b5 :) use xtcp

USE xtcp

Query id: 4d9809bd-de8d-4363-8216-bc7d4bd31b01

Ok.

0 rows in set. Elapsed: 0.002 sec.

5d1ddc0e72b5 :) show tables

SHOW TABLES

Query id: d35852fb-1cd5-48aa-af5f-540a95a97178

   ┌─name────────────────────┐
1. │ flat_xtcp_records_kafka │
2. │ xtcp_records            │
3. │ xtcp_records_mv         │
   └─────────────────────────┘

3 rows in set. Elapsed: 0.001 sec
```


### Clickhouse logs

```
[das@t:~/Downloads/xtcp2]$ docker exec -ti xtcp-clickhouse-1 ls /var/log/clickhouse-server
clickhouse-server.err.log  clickhouse-server.log

[das@t:~/Downloads/xtcp2]$ docker exec -ti xtcp-clickhouse-1 tail -f /var/log/clickhouse-server/clickhouse-server.err.log
17. DB::PipelineExecutor::executeStepImpl(unsigned long, std::atomic<bool>*) @ 0x00000000125c43b0
18. DB::PipelineExecutor::execute(unsigned long, bool) @ 0x00000000125c3842
19. DB::CompletedPipelineExecutor::execute() @ 0x00000000125c2152
20. DB::StorageKafka::threadFunc(unsigned long) @ 0x000000000fe27e7e
21. DB::BackgroundSchedulePool::threadFunction() @ 0x000000001035a0e0
22. void std::__function::__policy_invoker<void ()>::__call_impl<std::__function::__default_alloc_func<ThreadFromGlobalPoolImpl<false, true>::ThreadFromGlobalPoolImpl<DB::BackgroundSchedulePool::BackgroundSchedulePool(unsigned long, StrongTypedef<unsigned long, CurrentMetrics::MetricTag>, StrongTypedef<unsigned long, CurrentMetrics::MetricTag>, char const*)::$_0>(DB::BackgroundSchedulePool::BackgroundSchedulePool(unsigned long, StrongTypedef<unsigned long, CurrentMetrics::MetricTag>, StrongTypedef<unsigned long, CurrentMetrics::MetricTag>, char const*)::$_0&&)::'lambda'(), void ()>>(std::__function::__policy_storage const*) @ 0x000000001035b187
23. void* std::__thread_proxy[abi:v15007]<std::tuple<std::unique_ptr<std::__thread_struct, std::default_delete<std::__thread_struct>>, void ThreadPoolImpl<std::thread>::scheduleImpl<void>(std::function<void ()>, Priority, std::optional<unsigned long>, bool)::'lambda0'()>>(void*) @ 0x000000000d251a89
24. ? @ 0x00007f0a17fcc609
25. ? @ 0x00007f0a17ef1353
 (version 24.8.12.28 (official build))
```

Clickhouse troubleshooting: https://clickhouse.com/docs/knowledgebase/useful-queries-for-troubleshooting

### Inspect docker images

https://github.com/wagoodman/dive
```
[das@t:~/Downloads/xtcp2]$ dive randomizedcoder/xtcp_clickhouse
Image Source: docker://randomizedcoder/xtcp_clickhouse
Fetching image... (this can take a while for large images)
Analyzing image...
Building cache...
```


https://vincent.bernat.ch/en/blog/2023-dynamic-protobuf-golang

https://github.com/planetscale/vtprotobuf

https://pkg.go.dev/google.golang.org/protobuf@v1.36.3/encoding/protodelim