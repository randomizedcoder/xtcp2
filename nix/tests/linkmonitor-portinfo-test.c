/* Verify the pure-Go decoder's byte fixtures with the pinned rdma-core decoder. */
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <infiniband/mad.h>
#include <rdma/ib_user_ioctl_verbs.h>

_Static_assert(IB_UVERBS_PCF_EXTENDED_SPEEDS_SUP == (1 << 14), "extended speed capability");
_Static_assert(IB_UVERBS_PCF_LINK_SPEED_WIDTH_TABLE_SUP == (1 << 27), "speed/width constraints");

int main(void) {
    unsigned char data[64] = {0};
    data[29] = 1; data[30] = 2; data[31] = 16;
    data[32] = 0x74; data[35] = 0x42;
    data[56] = 0x52; data[62] = 0x88; data[63] = 0x1f;
    assert(mad_get_field(data, 0, IB_PORT_LINK_WIDTH_ENABLED_F) == 1);
    assert(mad_get_field(data, 0, IB_PORT_LINK_WIDTH_SUPPORTED_F) == 2);
    assert(mad_get_field(data, 0, IB_PORT_LINK_WIDTH_ACTIVE_F) == 16);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_SUPPORTED_F) == 7);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_ENABLED_F) == 2);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_ACTIVE_F) == 4);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_SUPPORTED_F) == 8);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_ACTIVE_F) == 8);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_ENABLED_F) == 31);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_SUPPORTED_2_F) == 2);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_ACTIVE_2_F) == 2);
    assert(mad_get_field(data, 0, IB_PORT_LINK_SPEED_EXT_ENABLED_2_F) == 2);
    puts("positive/boundary: pinned PortInfo field decoding matches Go fixtures");
    return 0;
}
