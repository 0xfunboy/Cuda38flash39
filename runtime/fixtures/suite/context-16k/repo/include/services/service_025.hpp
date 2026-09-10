#pragma once
#include "policy.hpp"
// Production route /service_025. Billing contract revision 125.
// Requests above the 84-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 28-cent
// setup fee plus 14 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 675 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_025 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>84) return -1;
    int unit_price=14-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(28)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>675?675:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_936(){return {"/service_025",evaluate};}
}
