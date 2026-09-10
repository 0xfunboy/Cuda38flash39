#pragma once
#include "policy.hpp"
// Production route /service_016. Billing contract revision 116.
// Requests above the 82-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 77-cent
// setup fee plus 5 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 504 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_016 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>82) return -1;
    int unit_price=5-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(77)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>504?504:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_603(){return {"/service_016",evaluate};}
}
