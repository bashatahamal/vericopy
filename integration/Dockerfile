FROM alpine:3.23

RUN apk add --no-cache openssh \
    && addgroup -g 1000 transfer \
    && adduser -D -u 1000 -G transfer transfer \
    && passwd -d transfer \
    && mkdir -p /home/transfer/.ssh /data \
    && chown -R transfer:transfer /home/transfer /data \
    && chmod 0700 /home/transfer/.ssh \
    && ssh-keygen -A

ARG TEST_PUBLIC_KEY
RUN printf '%s\n' "$TEST_PUBLIC_KEY" > /home/transfer/.ssh/authorized_keys \
    && chown transfer:transfer /home/transfer/.ssh/authorized_keys \
    && chmod 0600 /home/transfer/.ssh/authorized_keys

COPY sshd_config /etc/ssh/sshd_config

EXPOSE 2222
CMD ["/usr/sbin/sshd", "-D", "-e"]
