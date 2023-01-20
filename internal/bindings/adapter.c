#include "adapter.h"
#include <stdlib.h>

void bridge_on_request_call(on_request_call f, char *src_id, char *src_email,
                            char *route_id, char *channel_id,
                            struct SaltpaneloOnRequestCallResponse *rv) {
  f(src_id, src_email, route_id, channel_id, rv);
}