#include "adapter.h"

struct SaltpaneloOnRequestCallResponse
bridge_on_request_call(on_request_call f, char *src_id, char *src_email,
                       char *route_id, char *channel_id) {
  return f(src_id, src_email, route_id, channel_id);
}