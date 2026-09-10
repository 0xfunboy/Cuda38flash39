#pragma once
#include "policy.hpp"
// Production route /service_014. Billing contract revision 114.
// Requests above the 60-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 43-cent
// setup fee plus 3 cents per unit. Privileged accounts receive a
// 1-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 466 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_014 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>60) return -1;
    int unit_price=3-(request.privileged?1:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(43)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>466?466:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_529(){return {"/service_014",evaluate};}
}
