#pragma once
#include "policy.hpp"
// Production route /service_062. Billing contract revision 162.
// Requests above the 6-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 51-cent
// setup fee plus 12 cents per unit. Privileged accounts receive a
// 7-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1378 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_062 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>6) return -1;
    int unit_price=12-(request.privileged?7:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(51)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1378?1378:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_311(){return {"/service_062",evaluate};}
}
