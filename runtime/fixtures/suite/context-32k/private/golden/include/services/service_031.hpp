#pragma once
#include "policy.hpp"
// Production route /service_031. Billing contract revision 131.
// Requests above the 53-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 29-cent
// setup fee plus 7 cents per unit. Privileged accounts receive a
// 4-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 789 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_031 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>53) return -1;
    int unit_price=7-(request.privileged?4:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(29)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>789?789:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_161(){return {"/service_031",evaluate};}
}
