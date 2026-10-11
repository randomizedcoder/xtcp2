from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from typing import ClassVar as _ClassVar

DESCRIPTOR: _descriptor.FileDescriptor

class Access(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ACCESS_UNSPECIFIED: _ClassVar[Access]
    ACCESS_READ: _ClassVar[Access]
    ACCESS_ADMIN: _ClassVar[Access]
ACCESS_UNSPECIFIED: Access
ACCESS_READ: Access
ACCESS_ADMIN: Access
ACCESS_FIELD_NUMBER: _ClassVar[int]
access: _descriptor.FieldDescriptor
