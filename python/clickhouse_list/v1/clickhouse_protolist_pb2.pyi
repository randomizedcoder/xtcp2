from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Record(_message.Message):
    __slots__ = ("my_uint32",)
    MY_UINT32_FIELD_NUMBER: _ClassVar[int]
    my_uint32: int
    def __init__(self, my_uint32: _Optional[int] = ...) -> None: ...

class Envelope(_message.Message):
    __slots__ = ("row",)
    ROW_FIELD_NUMBER: _ClassVar[int]
    row: _containers.RepeatedCompositeFieldContainer[Record]
    def __init__(self, row: _Optional[_Iterable[_Union[Record, _Mapping]]] = ...) -> None: ...
