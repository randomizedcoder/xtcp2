# grpcurl

This readme has some examples of using grpcurl to interface with xtcp's grpc interface




```
[nix-shell:~/Downloads/xtcp2/cmd/nsTest]$ grpcurl -plaintext -use-reflection hp1:8888 list xtcp_config.v1.ConfigService
xtcp_config.v1.ConfigService.Get
xtcp_config.v1.ConfigService.Set
xtcp_config.v1.ConfigService.SetPollFrequency
```

## ConfigService.Get
```
grpcurl -plaintext hp1:8888 xtcp_config.v1.ConfigService.Get
```

```
[nix-shell:~/Downloads/xtcp2/cmd/nsTest]$ grpcurl -plaintext hp1:8888 xtcp_config.v1.ConfigService.Get
{
  "config": {
    "nlTimeoutMilliseconds": "1000",
    "pollFrequency": "10s",
    "pollTimeout": "9s",
    "netlinkers": 4,
    "nlmsgSeq": 666,
    "packetSizeMply": 8,
    "capturePath": "./",
    "modulus": "1",
    "marshalTo": "proto",
    "dest": "null",
    "topic": "xtcp",
    "kafkaProduceTimeout": "0s",
    "debugLevel": 11,
    "grpcPort": 8888,
    "enabledDeserializers": {
      "enabled": {
        "cong": true,
        "info": true
      }
    }
  }
}
```



## ConfigService.SetPollFrequency
Example of using the SetPollFrequency

```
[nix-shell:~/Downloads/xtcp2/cmd/nsTest]$ grpcurl -plaintext -use-reflection hp1:8888 describe xtcp_config.v1.ConfigService.SetPollFrequency
xtcp_config.v1.ConfigService.SetPollFrequency is a method:
rpc SetPollFrequency ( .xtcp_config.v1.SetPollFrequencyRequest ) returns ( .xtcp_config.v1.SetPollFrequencyResponse ) {
  option (.google.api.http) = { put: "/ConfigService/SetPollFrequency", body: "*" };
}
```

```
[nix-shell:~/Downloads/xtcp2/cmd/nsTest]$ grpcurl -plaintext -use-reflection hp1:8888 describe xtcp_config.v1.SetPollFrequencyRequest
xtcp_config.v1.SetPollFrequencyRequest is a message:
message SetPollFrequencyRequest {
  option (.buf.validate.message) = {
    cel: [
      {
        id: "XtcpConfig.poll",
        message: "Poll timeout must be less than poll poll_frequency",
        expression: "this.poll_timeout < this.poll_frequency"
      }
    ]
  };
  .google.protobuf.Duration poll_frequency = 20 [
    (.buf.validate.field) = {
      duration: { lte: { seconds: 604800 }, gte: { } },
      required: true
    }
  ];
  .google.protobuf.Duration poll_timeout = 30 [
    (.buf.validate.field) = {
      duration: { lte: { seconds: 604800 }, gte: { } },
      required: true
    }
  ];
}
```

Change the poll frequency to 20s
```
grpcurl -plaintext -d @ hp1:8888 xtcp_config.v1.ConfigService.SetPollFrequency <<EOM
{
  "poll_frequency": "20s",
  "poll_timeout": "9s"
}
EOM
```

Change the poll frequency to 2s
```
grpcurl -plaintext -d @ hp1:8888 xtcp_config.v1.ConfigService.SetPollFrequency <<EOM
{
  "poll_frequency": "2s",
  "poll_timeout": "1.5s"
}
EOM
```


grpcurl -plaintext hp1:8888 xtcp_config.v1.ConfigService.Get
