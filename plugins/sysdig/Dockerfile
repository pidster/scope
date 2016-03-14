FROM sysdig/sysdig

COPY ./chisels/* /usr/share/sysdig/chisels/
COPY ./sysdig-plugin /usr/bin/sysdig-plugin
CMD ["/usr/bin/sysdig-plugin"]
