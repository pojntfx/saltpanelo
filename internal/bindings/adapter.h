struct SaltpaneloOnRequestCallResponse {
  char Accept;
  char *Err;
};

typedef void (*on_request_call)(char *src_id, char *src_email, char *route_id,
                                char *channel_id,
                                struct SaltpaneloOnRequestCallResponse *rv);

typedef void (*on_call_disconnected)(char *route_id, char **rv);

void bridge_on_request_call(on_request_call f, char *src_id, char *src_email,
                            char *route_id, char *channel_id,
                            struct SaltpaneloOnRequestCallResponse *rv);

void bridge_on_call_disconnected(on_call_disconnected f, char *route_id,
                                 char **rv);