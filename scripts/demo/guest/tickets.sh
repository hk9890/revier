# A tab that looks like a ticket viewer, for the tickets target's key.
# Usage: tickets.sh <project>
printf '\033]2;tickets\033\\'
clear
printf '\033[?25l'
printf '\n \033[1m%s\033[0m  \033[2mopen tickets\033[0m\n\n' "$1"
row() { printf ' \033[38;5;%sm%-12s\033[0m %-8s %s\n' "$1" "$2" "$3" "$4"; }
row 209 "in progress" "WEB-142" "Checkout page: fix tax rounding"
row 111 "ready" "WEB-139" "Show delivery dates in the cart"
row 111 "ready" "WEB-151" "Coupon field rejects lower case codes"
row 245 "backlog" "WEB-117" "Move product images to the CDN"
row 245 "backlog" "WEB-160" "Dark mode for the order history"
printf '\n \033[2menter open   n new   / filter   q quit\033[0m\n'
exec sleep 3600
