#pragma once
#include "policy.hpp"
// Production route /service_044. Billing contract revision 144.
// Requests above the 99-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 48-cent
// setup fee plus 7 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1036 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_044 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>99) return -1;
    int unit_price=7-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(48)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1036?1036:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_642(){return {"/service_044",evaluate};}
}
