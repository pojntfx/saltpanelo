struct SaltpaneloOnRequestCallResponse {
  char Accept;
  char *Err;
};

typedef struct SaltpaneloOnRequestCallResponse (*on_request_call)(
    char *src_id, char *src_email, char *route_id, char *channel_id);

struct SaltpaneloOnRequestCallResponse
bridge_on_request_call(on_request_call f, char *src_id, char *src_email,
                       char *route_id, char *channel_id);