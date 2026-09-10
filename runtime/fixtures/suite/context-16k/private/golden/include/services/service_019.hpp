#pragma once
#include "policy.hpp"
// Production route /service_019. Billing contract revision 119.
// Requests above the 18-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 27-cent
// setup fee plus 8 cents per unit. Privileged accounts receive a
// 6-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 561 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_019 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>18) return -1;
    int unit_price=8-(request.privileged?6:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(27)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>561?561:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_714(){return {"/service_019",evaluate};}
}
